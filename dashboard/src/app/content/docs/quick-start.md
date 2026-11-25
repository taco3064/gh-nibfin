---
title: Quick start
description: Install gh nibfin, add Runners, log in, prepare a repository and run the first Issue.
---

This guide takes one repository from nothing to its first Issue running on your
own Runners.

## Requirements

- The [GitHub CLI](https://cli.github.com/), logged in with `gh auth login`.
  `init` needs the `workflow` scope: `gh auth refresh -s workflow`.
- Admin access to the repository you prepare.
- [Podman](https://podman.io/docs/installation). On Windows and macOS, also a
  `podman machine` (`podman machine init`); the Runners live in that machine.
- A Claude or Codex account for the agents' provider CLI.
- To build gh nibfin from source: [Go](https://go.dev/dl/) and
  [Node.js](https://nodejs.org/) 24.

## 1. Install

gh nibfin has no release yet, so build it from source and install the checkout
as a local extension:

```sh
git clone https://github.com/taco3064/gh-nibfin.git
cd gh-nibfin/dashboard
npm ci
npm run build
cd ..
go build
gh extension install .
```

`npm run build` builds the Dashboard UI that `go build` embeds. `gh nibfin`
then runs the binary in this folder.

## 2. Add Runners

In a clone of the repository whose Runners run the agents:

```sh
gh nibfin runner add claude
```

Use `codex` for Codex. This adds two Runners (`--count N` for another number)
on the Linux host, or in the default `podman machine` on Windows and macOS. Each
is labelled `nibfin-<user id>` for your GitHub user, so the Issues assigned to you
run on it. It shows its plan and asks before it changes anything. A collaborator
with write access gets their registration through a request Issue; see the
[README](https://github.com/taco3064/gh-nibfin#runners).

## 3. Log in

```sh
gh nibfin login
```

This runs the provider CLI's own login where the Runners run, for every provider
with Runners that is not logged in yet. gh nibfin stores no token.

## 4. Prepare the repository

```sh
gh nibfin init -R OWNER/REPO
```

`init` creates the labels `scope`, `shaped`, `delivered`, `pause` and `pending`,
sets the repository's Nibfin settings, stores your gh login as the Secret
`NIBFIN_GITHUB_TOKEN`, and opens one pull request from the branch `nibfin/init`
with the Nibfin workflows. Then:

1. Merge the `nibfin/init` pull request.
2. Open the Settings link `init` prints and set Issues "Creation allowed by" to
   Collaborators only. GitHub offers this only in the web UI.

## 5. Run the first Issue

1. Create an Issue that states the outcome you want.
2. Add the `scope` label.
3. Assign exactly one person whose Runners are registered, for example yourself.

Nibfin then shapes the Issue, delivers it as a pull request, reviews and merges
it, and closes it with `delivered` once CD passes. Follow it with:

```sh
gh nibfin status -R OWNER/REPO
gh nibfin dashboard
```

To stop an Issue, run `gh nibfin pause <issue>`; `gh nibfin resume <issue>`
continues it. [How it works](../how-it-works/) explains each stage and label.
