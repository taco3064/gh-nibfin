package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
)

// Registration and backing identity are private comparison data, never diagnostics.
type sparseObservation struct {
	Binding    string `json:"-"`
	Setting    string `json:"setting"`
	Capability string `json:"capability"`
}

type sparseProvider interface {
	ObserveSparse(string) sparseObservation
	EnableSparse(context.Context, string) string
}

type sparseReport struct {
	Experimental       bool              `json:"experimental"`
	Observation        sparseObservation `json:"observation"`
	Choice             string            `json:"choice"`
	Result             string            `json:"result"`
	Operation          string            `json:"operation,omitempty"`
	SupportedOperation []string          `json:"supported_operation,omitempty"`
	Attempted          bool              `json:"attempted"`
	Guidance           string            `json:"guidance"`
}

func sparseManageArguments(name string) []string {
	return []string{"--manage", name, "--set-sparse", "true"}
}

func observeSparse(p Process, t desktopTarget) sparseObservation {
	o := sparseObservation{Setting: "unknown", Capability: "unavailable"}
	if t.Provider == "wsl" && t.Version == 2 {
		if provider, ok := p.(sparseProvider); ok {
			o = provider.ObserveSparse(t.Name)
		}
	}
	return o
}

func sparsePlan(t desktopTarget, o desktopOptions) sparseReport {
	result := "unavailable"
	switch {
	case o.Sparse.Setting == "enabled":
		result = "enabled"
	case o.Reclamation == "unchanged":
		result = "unchanged"
	case t.Running:
		result = "deferred_running"
	case o.Sparse.Capability == "permission_denied":
		result = "permission_denied"
	case o.Sparse.Capability == "unsupported":
		result = "unsupported"
	case o.Sparse.Capability == "supported" && o.Sparse.Setting == "disabled":
		result = "enable_after_explicit_consent"
	}
	report := sparseReport{Experimental: true, Observation: o.Sparse,
		Choice: o.Reclamation, Result: result,
		Guidance: "Resume Windows init on this selected target after it naturally stops; " +
			"installation may continue. Sparse configuration does not prove physical recovery."}
	if o.Sparse.Capability == "supported" {
		report.SupportedOperation = append([]string{"wsl.exe"},
			sparseManageArguments(t.Name)...)
	}
	return report
}

func applySparse(ctx context.Context, p Process, t desktopTarget,
	o desktopOptions, out io.Writer) (resultErr error) {
	if t.Provider != "wsl" || t.Version != 2 {
		return nil
	}
	report := sparsePlan(t, o)
	defer func() {
		if err := json.NewEncoder(out).Encode(map[string]any{
			"reclamation": report}); err != nil && resultErr == nil {
			resultErr = errors.New("OUTPUT_UNAVAILABLE_AFTER_RECLAMATION")
		}
	}()
	if o.Reclamation != "enable" || !o.SparseConsent {
		report.Result = "unchanged"
		return nil
	}
	provider, ok := p.(sparseProvider)
	if !ok {
		return nil
	}
	fresh := provider.ObserveSparse(t.Name)
	report.Observation = fresh
	if o.Sparse.Binding != "" && fresh.Binding != "" &&
		fresh.Binding != o.Sparse.Binding {
		report.Result = "target_changed"
		return errors.New("GUEST_TARGET_CHANGED")
	}
	if fresh.Setting != "enabled" && t.Running {
		report.Result = "deferred_running"
		return nil
	}
	if fresh.Capability == "permission_denied" && fresh.Setting == "unknown" {
		report.Result = "permission_denied"
		return nil
	}
	if fresh.Binding == "" || o.Sparse.Binding == "" || fresh.Setting == "unknown" {
		report.Result = "unknown_read_back_required"
		return nil
	}
	if fresh.Setting == "enabled" {
		report.Result = "enabled"
		return nil
	}
	if fresh.Capability != "supported" {
		report.Result = fresh.Capability
		return nil
	}
	// This inventory does not enter any guest, including running guests.
	targets, err := wslInventory(p)
	if err != nil {
		report.Result = "unknown_state"
		return nil
	}
	found := false
	for _, current := range targets {
		if current.Name != t.Name {
			continue
		}
		found = true
		if current.Version != 2 {
			report.Result = "target_changed"
			return errors.New("GUEST_TARGET_CHANGED")
		}
		if current.Running {
			report.Result = "deferred_running"
			return nil
		}
	}
	if !found {
		report.Result = "target_changed"
		return errors.New("GUEST_TARGET_CHANGED")
	}
	report.Attempted = true
	result := provider.EnableSparse(ctx, t.Name)
	report.Operation = result
	after := provider.ObserveSparse(t.Name)
	report.Observation = after
	if after.Binding != "" && after.Binding != fresh.Binding {
		report.Result = "target_changed"
		return errors.New("GUEST_TARGET_CHANGED")
	}
	if after.Binding == fresh.Binding && after.Setting == "enabled" {
		report.Result = "enabled"
	} else if after.Binding == "" || after.Setting == "unknown" {
		report.Result = "unknown_read_back_required"
	} else if result == "succeeded" {
		report.Result = "not_confirmed"
	} else {
		report.Result = result
	}
	return nil
}
