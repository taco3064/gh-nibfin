package cmd

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/status"
)

// writeStatus renders the status Result as text. --json prints the Result
// itself.
func writeStatus(w io.Writer, r op.Result) {
	if h, ok := r["host"].(op.HostReport); ok {
		writeHost(w, h)
	}
	latest, _ := r["latestImage"].(status.LatestImage)
	if latest.Error != nil {
		writeErr(w, "latest image", latest.Error)
	} else {
		fmt.Fprintf(w, "latest image: %s\n", shortImage(latest.Ref))
	}

	if repo, ok := r["repo"].(string); ok {
		img, _ := r["image"].(status.RepoImage)
		fmt.Fprintf(w, "\n%s  image: %s\n", clean(repo), imageText(img))
		writeErr(w, "  image", img.Error)
		writeErr(w, "  issues", errOf(r["issuesError"]))
		issues, _ := r["issues"].([]status.Issue)
		writeIssues(w, issues)
		if run, ok := r["nibfinRun"].(status.NibfinRun); ok {
			writeRun(w, run)
		}
		return
	}

	who, _ := r["user"].(string)
	host, _ := r["githubHost"].(string)
	if who != "" {
		fmt.Fprintf(w, "\nscope Issues assigned to %s on %s:\n", clean(who), clean(host))
	} else {
		fmt.Fprintf(w, "\nscope Issues assigned to you on %s:\n", clean(host))
	}
	writeErr(w, "user", errOf(r["userError"]))
	writeErr(w, "issues", errOf(r["issuesError"]))
	repos, _ := r["repos"].([]status.RepoIssues)
	if len(repos) == 0 && r["issuesError"] == nil {
		fmt.Fprintln(w, "  none")
	}
	for _, ri := range repos {
		fmt.Fprintf(w, "%s  image: %s\n", clean(ri.Repo), imageText(ri.Image))
		writeErr(w, "  image", ri.Image.Error)
		writeIssues(w, ri.Issues)
	}
	if t, _ := r["truncated"].(bool); t {
		fmt.Fprintln(w, "(search returned only part of the results)")
	}
}

func errOf(v any) *op.Error {
	e, _ := v.(*op.Error)
	return e
}

func writeErr(w io.Writer, part string, e *op.Error) {
	if e != nil {
		fmt.Fprintf(w, "%s: %s: %s\n", part, e.Code, clean(e.Error()))
	}
}

func writeHost(w io.Writer, h op.HostReport) {
	if h.Host.Error != nil {
		writeErr(w, "host", h.Host.Error)
	} else {
		state := "stopped"
		if h.Host.Running {
			state = "running"
		}
		name := h.Host.Describe
		if h.Host.VMType != "" {
			name += " (" + h.Host.VMType + ")"
		}
		fmt.Fprintf(w, "host: %s, %s\n", clean(name), state)
	}
	if h.Memory.Error != nil {
		writeErr(w, "memory", h.Memory.Error)
	} else if h.Host.Error == nil {
		fmt.Fprintf(w, "memory: %s total, %s available\n", gb(h.Memory.TotalKB), gb(h.Memory.AvailableKB))
	}
	if h.Runners.Error == nil || len(h.Runners.Runners) > 0 {
		if h.Runners.Repo != "" {
			fmt.Fprintf(w, "runners (%s):\n", clean(h.Runners.Repo))
		} else {
			fmt.Fprintln(w, "runners:")
		}
		if len(h.Runners.Runners) == 0 {
			fmt.Fprintln(w, "  none")
		}
		for _, rs := range h.Runners.Runners {
			active := "inactive"
			if rs.Active {
				active = "active"
			}
			gh := "not listed"
			switch {
			case h.Runners.GitHubError != nil || h.Runners.Repo == "":
				gh = "unknown"
			case rs.Listed:
				gh = rs.Status
				if rs.Busy {
					gh += "  busy"
				} else {
					gh += "  idle"
				}
			}
			fmt.Fprintf(w, "  %s  %s  github: %s\n", clean(rs.Slot), active, clean(gh))
		}
	}
	writeErr(w, "runners", h.Runners.Error)
	writeErr(w, "runners github", h.Runners.GitHubError)
	for _, p := range h.Providers {
		if p.Error != nil {
			writeErr(w, "login "+p.Provider, p.Error)
			continue
		}
		state := "not logged in"
		if p.LoggedIn {
			state = "logged in"
		}
		fmt.Fprintf(w, "login %s: %s\n", p.Provider, state)
	}
}

func gb(kb int64) string {
	if kb <= 0 {
		return "?"
	}
	return fmt.Sprintf("%.1f GB", float64(kb)/(1<<20))
}

// shortImage shows the digest of ghcr.io/taco3064/nibfin@sha256:<hex>.
func shortImage(ref string) string {
	_, d, ok := strings.Cut(ref, "@")
	if !ok {
		return clean(ref)
	}
	if len(d) > len("sha256:")+12 {
		d = d[:len("sha256:")+12]
	}
	return d
}

func imageText(img status.RepoImage) string {
	switch {
	case img.Error != nil:
		return "unreadable"
	case img.Set == "peer", img.Set == "none":
		return img.Set
	case img.Ref == nil:
		return "unknown (execution)"
	}
	state := "unknown"
	if img.Latest != nil {
		state = "not latest"
		if *img.Latest {
			state = "latest"
		}
	}
	return shortImage(*img.Ref) + " (" + state + ")"
}

func writeIssues(w io.Writer, issues []status.Issue) {
	for _, is := range issues {
		line := fmt.Sprintf("  #%d %s", is.Number, clean(is.Title))
		if is.Pause {
			line += "  [pause]"
		}
		if is.Pending {
			switch {
			case is.PendingComment != "":
				line += "  [pending " + clean(is.PendingComment) + "]"
			case is.PendingCommentError == nil:
				line += "  [pending (no marked comment)]"
			default:
				line += "  [pending]"
			}
		}
		fmt.Fprintln(w, line)
		writeErr(w, "    pending comment", is.PendingCommentError)
	}
}

func writeRun(w io.Writer, run status.NibfinRun) {
	switch {
	case run.Error != nil:
		writeErr(w, "nibfin run", run.Error)
		return
	case !run.Initialized:
		fmt.Fprintln(w, "nibfin run: no init commit (no generated nibfin-issue.yml on the default branch)")
		return
	}
	since := "init commit " + run.InitCommit
	if run.InitPR != nil {
		since = fmt.Sprintf("init PR #%d %s", run.InitPR.Number, clean(run.InitPR.URL))
	}
	if run.Succeeded {
		fmt.Fprintf(w, "nibfin run: succeeded %s at %s, after %s merged at %s\n", clean(run.Run), clean(run.At), since, run.MergedAt)
	} else {
		fmt.Fprintf(w, "nibfin run: none succeeded since %s merged at %s\n", since, run.MergedAt)
	}
}

// clean drops control characters, so text read from GitHub cannot move the
// cursor or recolour the terminal.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
