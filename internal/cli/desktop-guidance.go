package cli

import "strings"

func desktopGuidance(host, repo string) []string {
	guide := []string{}
	if host == "windows" {
		guide = append(guide,
			"Operator setup: wsl --install -d Ubuntu-24.04",
			"https://learn.microsoft.com/en-us/windows/wsl/basic-commands",
			"WSL 1 cannot be used. Select an existing WSL 2 Ubuntu 24.04 guest; "+
				"conversion and kernel configuration are operator actions.",
			"WSL running state is a snapshot. systemd alone does not keep WSL alive.",
			"Enter: wsl.exe --distribution Ubuntu-24.04 --cd ~")
	} else {
		guide = append(guide,
			"Install Canonical Multipass using its official macOS package:",
			"https://canonical.com/multipass/docs/latest/how-to-guides/install-multipass/",
			"Operator setup: multipass launch 24.04 --name nibfin",
			"Enter: multipass shell nibfin",
			"Alternatively run the Linux CLI directly inside an equivalent native "+
				"Ubuntu 24.04 arm64 VM. Multipass discovery is optional.",
			"macOS sleep or guest shutdown interrupts execution.")
	}
	return append(guide,
		"Inside the guest, use gh-nibfin_linux_amd64 for native amd64 or "+
			"gh-nibfin_linux_arm64 for native arm64 (required on Apple Silicon).",
		"Place the matching executable as gh-nibfin in a separate gh-nibfin directory; "+
			"inside that directory run chmod +x gh-nibfin then gh extension install .",
		"Use a locally prepared verified executable; no published download is promised.",
		"Inside the guest, if needed run the official gh auth login and codex login. "+
			"Credentials remain inside the guest; desktop logins are not copied.",
		"Guest preflight: gh nibfin preflight --repo "+repo,
		"Guest init: gh nibfin init --repo "+repo+" --apply",
		"Opening consent does not authorize managed setup: guest-local consent and "+
			"selected-account checks still apply.",
		"After disconnect, resume init inside the same guest for durable intent and "+
			"relay read-back. Unknown effects remain pending; do not replay mutations "+
			"or start another Writer.")
}

func desktopResume(t desktopTarget, o desktopOptions) []string {
	repo := o.Repo
	entry := "multipass shell '" + t.Name + "'"
	if t.Provider == "wsl" {
		entry = "wsl.exe --distribution '" + t.Name + "' --cd ~"
	}
	resume := append([]string(nil), o.Forward...)
	if len(resume) > 0 && resume[0] == "preflight" {
		resume = []string{"init", "--repo", repo, "--apply"}
	}
	hasApply := false
	for i, arg := range resume {
		key, _, _ := strings.Cut(arg, "=")
		if key == "--apply" {
			resume[i], hasApply = "--apply", true
		}
	}
	if !hasApply {
		resume = append(resume, "--apply")
	}
	quoted := make([]string, len(resume))
	for i, arg := range resume {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
	}
	return []string{entry, "gh nibfin preflight --repo " + repo,
		"gh nibfin " + strings.Join(quoted, " "),
		"Desktop resume: gh nibfin " + strings.Join(quoted, " ") + " --target '" +
			t.Provider + ":" + t.Name + "' --consent-open-target"}
}
