package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type upgradeDesktopFake struct {
	running      bool
	adoption     string
	gap          string
	changed      bool
	observations int
	opens        int
	entered      []string
	output       string
	exit         error
	cancel       func()
}

func upgradeTestHost() sharedHost {
	host := sharedHost{SchemaVersion: 1, MemberID: 7,
		SharedExecution: SharedExecution{Repository: "o/r", RepositoryID: 10}}
	host.Runners = append(host.Runners, struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}{31, "one"}, struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}{32, "two"})
	host.AdoptedRepositoryIDs = []int64{10}
	return host
}

func upgradeTestObservation() upgradeGuestObservation {
	return upgradeGuestObservation{Repository: "o/r", RepositoryID: 10,
		Identity: desktopTestIdentity, Architecture: "amd64", Adoption: "observed",
		Installation: upgradeInstallation(upgradeTestHost())}
}

func (p *upgradeDesktopFake) Run(name string, args []string, _ []byte) ([]byte, error) {
	if name != "wsl.exe" {
		return nil, errors.New("unexpected tool")
	}
	if len(args) > 0 && args[0] == "--list" {
		text := "Ubuntu\n"
		if slices.Contains(args, "--verbose") {
			text = "NAME STATE VERSION\nUbuntu Stopped 2\n"
		}
		if slices.Contains(args, "--running") && !p.running {
			text = ""
		}
		return wslUTF16(text, false, false), nil
	}
	if len(args) < 6 || !slices.Equal(args[:5], []string{
		"--distribution", "Ubuntu", "--cd", "~", "--exec",
	}) {
		return nil, errors.New("unsafe argv")
	}
	command := args[5:]
	if slices.Equal(command, []string{"/usr/bin/true"}) {
		p.opens++
		p.running = true
		return nil, nil
	}
	if !p.running {
		return nil, errors.New("inspected stopped guest")
	}
	if len(command) == 5 && command[4] == overviewBootstrap {
		return json.Marshal(map[string]string{
			"entry": "/home/operator/.local/share/gh/extensions/gh-nibfin/gh-nibfin",
		})
	}
	if len(command) == 8 && command[4] == upgradeProbeScript &&
		command[6] == upgradeObserveCommand && command[7] == "o/r" {
		p.observations++
		if p.cancel != nil {
			p.cancel()
		}
		if p.gap != "" {
			return json.Marshal(map[string]string{"gap": p.gap})
		}
		o := upgradeTestObservation()
		if p.adoption != "" {
			o.Adoption = p.adoption
		}
		if p.changed && p.observations > 1 {
			o.Identity = strings.Repeat("a", 32)
		}
		return json.Marshal(o)
	}
	return nil, errors.New("unexpected guest query")
}

func (p *upgradeDesktopFake) EnterUpgradeGuest(ctx context.Context, name string,
	args []string, out io.Writer) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	p.entered = append([]string{name}, args...)
	_, _ = io.WriteString(out, p.output)
	return p.exit
}

func upgradeDesktopApp(t *testing.T, p *upgradeDesktopFake) (App, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "preferences.json")
	return App{Process: p, upgradePreferencePath: func() (string, error) {
		return path, nil
	}}, path
}

func TestUpgradeDesktopPreviewAndApplyPreserveGuestContract(t *testing.T) {
	for _, apply := range []bool{false, true} {
		p := &upgradeDesktopFake{running: true, output: "guest preview\n"}
		app, path := upgradeDesktopApp(t, p)
		args := []string{"--repo", "o/r", "--version", "v1.2.3", "--apply=false"}
		if apply {
			args[4] = "--apply=true"
		}
		var out bytes.Buffer
		if err := app.runDesktopUpgrade(args, &out); err != nil {
			t.Fatal(err)
		}
		if p.opens != 0 || p.observations != 2 || len(p.entered) == 0 {
			t.Fatal("missing read-only binding", p)
		}
		joined := strings.Join(p.entered, " ")
		for _, value := range []string{"--distribution Ubuntu --cd ~ --exec",
			"--version v1.2.3", args[4], "--linux-target-installation"} {
			if !strings.Contains(joined, value) {
				t.Fatal("lost argument", value, joined)
			}
		}
		if !strings.HasSuffix(out.String(), p.output) {
			t.Fatal(out.String())
		}
		var resume struct {
			Args []string `json:"resume_argv"`
		}
		if json.NewDecoder(strings.NewReader(out.String())).Decode(&resume) != nil ||
			len(resume.Args) < 3 || !slices.Equal(resume.Args[:3],
			[]string{"gh", "nibfin", "upgrade"}) ||
			!slices.Contains(resume.Args, args[4]) {
			t.Fatal("recovery changed the public command or intent", resume.Args)
		}
		_, err := os.Stat(path)
		if apply && err != nil || !apply && !errors.Is(err, os.ErrNotExist) {
			t.Fatal("preference mutation differs from apply intent", err)
		}
	}
}

func TestUpgradeDesktopStoppedGuestNeedsApplyAndOpeningConsent(t *testing.T) {
	for _, scenario := range []string{"preview", "apply", "consented"} {
		p := &upgradeDesktopFake{}
		app, _ := upgradeDesktopApp(t, p)
		args := []string{"--repo", "o/r", "--target", "wsl:Ubuntu"}
		if scenario != "preview" {
			args = append(args, "--apply")
		}
		if scenario == "consented" {
			args = append(args, "--consent-open-target")
		}
		err := app.runDesktopUpgrade(args, io.Discard)
		if scenario == "consented" {
			if err != nil || p.opens != 1 || len(p.entered) == 0 {
				t.Fatal(err, p)
			}
		} else if err == nil || p.opens != 0 || p.observations != 0 {
			t.Fatal("unconsented opening", err, p)
		}
	}
}

func TestUpgradeDesktopRefusesChangedUnadoptedAndUnavailableGuests(t *testing.T) {
	cases := []*upgradeDesktopFake{
		{running: true, changed: true},
		{running: true, adoption: "required"},
		{running: true, adoption: "unverified"},
		{running: true, gap: "INSTALLED_CLI_UPGRADE_TRANSPORT_UNSUPPORTED"},
		{running: true, gap: "GUEST_UPGRADE_OBSERVATION_UNAVAILABLE"},
	}
	for _, p := range cases {
		app, path := upgradeDesktopApp(t, p)
		err := app.runDesktopUpgrade([]string{"--repo", "o/r", "--apply"}, io.Discard)
		if err == nil || len(p.entered) != 0 {
			t.Fatal("delegated unsafe guest", err, p)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("remembered unverified target")
		}
	}
}

func TestUpgradeDesktopCancellationPreventsDelegation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &upgradeDesktopFake{running: true, cancel: cancel}
	app, path := upgradeDesktopApp(t, p)
	app.Context = ctx
	err := app.runDesktopUpgrade([]string{"--repo", "o/r", "--apply"}, io.Discard)
	if !errors.Is(err, context.Canceled) || len(p.entered) != 0 {
		t.Fatal(err, p)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled discovery wrote preferences")
	}
}
