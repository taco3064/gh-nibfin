package op

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/runners"
)

// The labels the Job template puts on each Job container.
const (
	JobRepoLabel  = "io.github.taco3064.nibfin.repo"
	JobIssueLabel = "io.github.taco3064.nibfin.issue"
)

// HostSlots is this host's Runner slots, the repository they serve and the
// running Job containers, read without GitHub.
type HostSlots struct {
	// Repo is the execution repository as HOST/OWNER/REPO; empty when no
	// slot is configured.
	Repo  string         `json:"repo,omitempty"`
	Slots []SlotDir      `json:"slots"`
	Error *Error         `json:"error,omitempty"`
	Jobs  []JobContainer `json:"jobs"`
	// JobsError is set when the Job containers could not be read.
	JobsError *Error `json:"jobsError,omitempty"`
}

// SlotDir is one Runner slot and its directory on the host.
type SlotDir struct {
	Slot     string `json:"slot"`
	Provider string `json:"provider"`
	Name     string `json:"name,omitempty"`
	Dir      string `json:"-"`
}

// JobContainer is one running Job container: its labels as podman reports
// them (unvalidated), when it started and the sources of its bind mounts.
type JobContainer struct {
	ID        string   `json:"-"`
	Repo      string   `json:"repo"`
	Issue     string   `json:"issue"`
	StartedAt string   `json:"startedAt,omitempty"`
	Mounts    []string `json:"-"`
}

// ReadHostSlots reads the Runner slots and the running Job containers. It
// only reads: a stopped machine is reported, never started; no GitHub read
// and no login check runs.
func ReadHostSlots(ctx context.Context) HostSlots {
	rep := HostSlots{Slots: []SlotDir{}, Jobs: []JobContainer{}}
	h, err := detectHost(ctx)
	if err != nil {
		rep.Error = AsError(err)
		rep.JobsError = rep.Error
		return rep
	}
	if h.NeedsStart() {
		rep.Error = AsError(Errorf(CodeMachineStopped, "%s is stopped; the Dashboard does not start it", h.Describe()))
		rep.JobsError = rep.Error
		return rep
	}
	if inv, err := listRunners(ctx, h); err != nil {
		rep.Error = AsError(err)
	} else {
		for _, r := range inv.Runners {
			rep.Slots = append(rep.Slots, SlotDir{Slot: r.Slot, Provider: r.Provider, Name: r.Name, Dir: r.Dir})
		}
		if repo, err := runners.Repo(inv.Runners); err != nil {
			rep.Error = AsError(Wrap(CodeRunnerRepoConflict, err))
		} else {
			rep.Repo = repo
		}
	}
	jobs, err := jobContainers(ctx, h)
	if err != nil {
		rep.JobsError = AsError(err)
	} else {
		rep.Jobs = jobs
	}
	return rep
}

// jobContainers lists the running containers that carry the Job template's
// repository label and inspects them.
func jobContainers(ctx context.Context, h runnerHost) ([]JobContainer, error) {
	// One filter: how several combine is not documented.
	out, err := podmanOut(ctx, h, "ps", "--quiet", "--no-trunc", "--filter", "label="+JobRepoLabel)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return []JobContainer{}, nil
	}
	out, err = podmanOut(ctx, h, append([]string{"container", "inspect"}, ids...)...)
	if err != nil {
		return nil, err
	}
	return parseInspect(out)
}

// parseInspect reads podman container inspect's JSON. A container without
// the repository label is skipped.
func parseInspect(b []byte) ([]JobContainer, error) {
	var list []struct {
		ID     string `json:"Id"`
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		State struct {
			Running   bool   `json:"Running"`
			StartedAt string `json:"StartedAt"`
		} `json:"State"`
		Mounts []struct {
			Type   string `json:"Type"`
			Source string `json:"Source"`
		} `json:"Mounts"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, Errorf(CodePodmanFailed, "podman container inspect: %v", err)
	}
	out := []JobContainer{}
	for _, c := range list {
		repo, ok := c.Config.Labels[JobRepoLabel]
		if !ok || !c.State.Running {
			continue
		}
		j := JobContainer{ID: c.ID, Repo: repo, Issue: c.Config.Labels[JobIssueLabel], StartedAt: c.State.StartedAt}
		for _, m := range c.Mounts {
			if m.Source != "" && (m.Type == "" || m.Type == "bind") {
				j.Mounts = append(j.Mounts, m.Source)
			}
		}
		out = append(out, j)
	}
	return out, nil
}

// podmanOut runs a podman command on h and returns its standard output.
func podmanOut(ctx context.Context, h runnerHost, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	c := h.Podman(ctx, args...)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, Errorf(CodeCancelled, "cancelled")
		}
		return nil, Errorf(CodePodmanFailed, "podman %s: %s", strings.Join(args[:min(2, len(args))], " "), stderrText(err, &stderr))
	}
	return stdout.Bytes(), nil
}
