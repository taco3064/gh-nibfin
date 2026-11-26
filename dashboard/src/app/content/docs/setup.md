---
title: Setup and management
description: Adding and removing Runners, logging in, preparing and upgrading repositories.
---

This page is not written yet. Until it is, the
[gh nibfin README](https://github.com/taco3064/gh-nibfin#readme) documents
[Runners](https://github.com/taco3064/gh-nibfin#runners),
[Login](https://github.com/taco3064/gh-nibfin#login),
[Init](https://github.com/taco3064/gh-nibfin#init) and
[Upgrade](https://github.com/taco3064/gh-nibfin#upgrade).

## Choosing a Runner's model

Each Runner chooses the model its Jobs use, the same way it chooses its
provider:

- Add `NIBFIN_MODEL=<model>` to a Runner's `.env` to set the model for that
  Runner only. Without it, Core's default for the Runner's provider applies
  (Claude: Opus; Codex: the Codex CLI's own default). `runner add` does not
  write this line, and `runner add` and `upgrade` keep it.
- The runner reads its `.env` when it starts, so restart the Runner after a
  change.
- No repository variable chooses the model. An old `NIBFIN_MODEL` repository
  variable is unused and can be deleted.
