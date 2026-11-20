package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

type sparseFake struct {
	observations     []sparseObservation
	reads, mutations int
	running          bool
	result           string
	calls            []string
}

func (p *sparseFake) ObserveSparse(name string) sparseObservation {
	p.calls = append(p.calls, "observe:"+name)
	i := p.reads
	p.reads++
	if i >= len(p.observations) {
		i = len(p.observations) - 1
	}
	return p.observations[i]
}

func (p *sparseFake) EnableSparse(_ context.Context, name string) string {
	p.mutations++
	p.calls = append(p.calls, "enable:"+name)
	return p.result
}

func (p *sparseFake) Run(tool string, args []string, _ []byte) ([]byte, error) {
	command := strings.Join(args, " ")
	p.calls = append(p.calls, command)
	if tool != "wsl.exe" {
		return nil, errors.New("unexpected tool")
	}
	switch command {
	case "--list --quiet":
		return wslUTF16("Ubuntu\n", false, true), nil
	case "--list --verbose":
		return wslUTF16("NAME STATE VERSION\nUbuntu Stopped 2\n", false, true), nil
	case "--list --running --quiet":
		active := ""
		if p.running {
			active = "Ubuntu\n"
		}
		return wslUTF16(active, false, true), nil
	case "--distribution Ubuntu --cd ~ --exec true":
		return nil, errors.New("fixture stops after guest opening")
	}
	return nil, errors.New("unexpected command")
}

func (*sparseFake) EnterGuest(context.Context, string, []string, io.Writer) error {
	return errors.New("unexpected terminal")
}

func TestSparseOutcomes(t *testing.T) {
	disabled := sparseObservation{"registration|file", "disabled", "supported"}
	enabled := disabled
	enabled.Setting = "enabled"
	unsupported := disabled
	unsupported.Capability = "unsupported"
	unknown := sparseObservation{Setting: "unknown", Capability: "unavailable"}
	replaced := disabled
	replaced.Binding = "replacement"
	replacedFile := disabled
	replacedFile.Binding = "registration|replacement-file"
	tests := []struct {
		name                    string
		fresh, after            sparseObservation
		running                 bool
		choice, operation, want string
		mutations               int
		changed                 bool
	}{
		{"stopped", disabled, enabled, false, "enable", "succeeded", "enabled", 1, false},
		{"already", enabled, enabled, false, "enable", "", "enabled", 0, false},
		{"running", disabled, disabled, true, "enable", "", "deferred_running", 0, false},
		{"unsupported", unsupported, unsupported, false, "enable", "", "unsupported", 0,
			false},
		{"denied", disabled, disabled, false, "enable", "permission_denied",
			"permission_denied", 1, false},
		{"failed", disabled, disabled, false, "enable", "operation_failed",
			"operation_failed", 1, false},
		{"readback", disabled, unknown, false, "enable", "succeeded",
			"unknown_read_back_required", 1, false},
		{"unconfirmed", disabled, disabled, false, "enable", "succeeded",
			"not_confirmed", 1, false},
		{"unchanged", disabled, disabled, false, "unchanged", "", "unchanged", 0, false},
		{"replacement", replaced, replaced, false, "enable", "", "", 0, true},
		{"replaced_file", replacedFile, replacedFile, false, "enable", "", "", 0, true},
		{"replaced_after", disabled, replaced, false, "enable", "succeeded", "", 1, true},
		{"lost_response", disabled, enabled, false, "enable", "operation_failed",
			"enabled", 1, false},
		{"lost_local_result", enabled, enabled, false, "enable", "", "enabled", 0, false},
		{"unknown", unknown, unknown, false, "enable", "", "unknown_read_back_required",
			0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &sparseFake{observations: []sparseObservation{tt.fresh, tt.after},
				running: tt.running, result: tt.operation}
			o := desktopOptions{Reclamation: tt.choice, SparseConsent: true, Sparse: disabled}
			var out bytes.Buffer
			err := applySparse(context.Background(), p,
				desktopTarget{Provider: "wsl", Name: "Ubuntu", Version: 2}, o, &out)
			if (err != nil) != tt.changed || p.mutations != tt.mutations {
				t.Fatalf("err=%v mutations=%d output=%s", err, p.mutations, out.String())
			}
			if tt.want != "" && !strings.Contains(out.String(), `"result":"`+tt.want+`"`) {
				t.Fatal(out.String())
			}
			if strings.Contains(out.String(), "registration|file") {
				t.Fatal("binding leaked")
			}
		})
	}
}

func TestSparsePublicChoiceAndPreview(t *testing.T) {
	args := []string{"init", "--repo", "owner/repo", "--target", "wsl:Ubuntu",
		"--reclamation", "unchanged", "--consent-experimental-sparse"}
	o, err := parseDesktopOptions(args)
	if err != nil || !slices.Equal(o.Forward, []string{"init", "--repo", "owner/repo"}) {
		t.Fatalf("%+v %v", o, err)
	}
	resume := strings.Join(desktopResume(desktopTarget{Provider: "wsl", Name: "Ubuntu"},
		o), "\n")
	if !strings.Contains(resume, "--reclamation unchanged") {
		t.Fatal(resume)
	}
	for _, option := range []string{"--reclamation=invalid",
		"--consent-experimental-sparse=maybe"} {
		if _, err := parseDesktopOptions(append(args[:5:5], option)); err == nil {
			t.Fatal("accepted invalid sparse argument")
		}
	}
	p := &sparseFake{observations: []sparseObservation{
		{"binding", "disabled", "supported"}}}
	var out bytes.Buffer
	a := App{Process: p}
	if err := a.runDesktop(args[:5], "windows", &out); err != nil {
		t.Fatal(err)
	}
	if p.mutations != 0 || !strings.Contains(out.String(), "experimental") ||
		!strings.Contains(out.String(), "supported_operation") ||
		!strings.Contains(out.String(), "--set-sparse") {
		t.Fatal(out.String())
	}
	out.Reset()
	apply := append(args[:5:5], "--apply", "--consent-open-target")
	if err := a.runDesktop(apply, "windows", &out); err == nil ||
		!strings.Contains(err.Error(), "SPARSE_CHOICE_REQUIRED") {
		t.Fatal(err)
	}
	if p.mutations != 0 {
		t.Fatal("opening consent authorized sparse conversion")
	}
}

func TestSparseHandoffBeforeOpening(t *testing.T) {
	disabled := sparseObservation{"binding", "disabled", "supported"}
	enabled := disabled
	enabled.Setting = "enabled"
	p := &sparseFake{observations: []sparseObservation{disabled, disabled, enabled},
		result: "succeeded"}
	a := App{Process: p}
	var out bytes.Buffer
	err := a.runDesktop([]string{"init", "--repo", "owner/repo", "--apply",
		"--consent-open-target", "--consent-experimental-sparse"}, "windows", &out)
	if err == nil || p.mutations != 1 {
		t.Fatalf("%v %s", err, out.String())
	}
	mutation := slices.Index(p.calls, "enable:Ubuntu")
	opening := slices.Index(p.calls, "--distribution Ubuntu --cd ~ --exec true")
	if mutation < 0 || opening <= mutation {
		t.Fatal(p.calls)
	}
}

type sparseHandoffFake struct {
	sparseFake
	guest desktopFake
}

func (p *sparseHandoffFake) Run(tool string, args []string, in []byte) ([]byte, error) {
	if len(args) >= 6 && args[0] == "--distribution" {
		if slices.Equal(args[5:], []string{"true"}) {
			p.running = true
			p.calls = append(p.calls, "open:Ubuntu")
			return nil, nil
		}
		command := append([]string{"exec", "guest", "--"}, args[5:]...)
		return p.guest.Run("multipass", command, in)
	}
	return p.sparseFake.Run(tool, args, in)
}

func (p *sparseHandoffFake) EnterGuest(ctx context.Context, tool string,
	args []string, out io.Writer) error {
	return p.guest.EnterGuest(ctx, tool, args, out)
}

func TestSparsePublicHandoffContinues(t *testing.T) {
	for _, scenario := range []string{"enable", "unchanged", "unsupported", "denied",
		"running", "unknown", "readback", "enabled"} {
		t.Run(scenario, func(t *testing.T) {
			disabled := sparseObservation{"binding", "disabled", "supported"}
			enabled := disabled
			enabled.Setting = "enabled"
			p := &sparseHandoffFake{
				sparseFake: sparseFake{observations: []sparseObservation{disabled, disabled,
					enabled}, result: "succeeded"},
				guest: desktopFake{CLI: true, Arch: "arm64", Identity: desktopTestIdentity},
			}
			args := []string{"init", "--repo", "o/r", "--target", "wsl:Ubuntu", "--apply",
				"--consent-open-target"}
			if scenario == "unchanged" {
				args = append(args, "--reclamation", "unchanged")
			} else {
				args = append(args, "--consent-experimental-sparse")
			}
			switch scenario {
			case "unsupported":
				p.observations = []sparseObservation{{"binding", "disabled", "unsupported"}}
			case "denied":
				p.result = "permission_denied"
				p.observations[2] = disabled
			case "running":
				p.running = true
			case "unknown":
				p.observations = []sparseObservation{{Setting: "unknown", Capability: "unavailable"}}
			case "readback":
				p.observations[2] = sparseObservation{Setting: "unknown", Capability: "unavailable"}
			case "enabled":
				p.observations = []sparseObservation{enabled}
			}
			var out bytes.Buffer
			err := (App{Process: p}).runDesktop(args, "windows", &out)
			if err != nil || len(p.guest.Entered) == 0 {
				t.Fatalf("%v %s", err, out.String())
			}
			for _, arg := range p.guest.Entered {
				if strings.Contains(arg, "reclamation") || strings.Contains(arg, "sparse") {
					t.Fatal("host option forwarded", p.guest.Entered)
				}
			}
			if scenario == "enable" {
				mutation := slices.Index(p.calls, "enable:Ubuntu")
				opening := slices.Index(p.calls, "open:Ubuntu")
				if mutation < 0 || opening <= mutation {
					t.Fatal(p.calls)
				}
			}
			if (scenario == "unchanged" || scenario == "running" || scenario == "enabled") &&
				p.mutations != 0 {
				t.Fatal("unexpected mutation", p.calls)
			}
		})
	}
}
