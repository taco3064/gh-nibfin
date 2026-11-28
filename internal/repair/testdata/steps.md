<!-- nibfin:repair -->
<!-- nibfin:repair-state for=0782fc37402f1d0f status=needs-person action=advice handled=AGENT_USAGE_LIMIT,NONE -->
Nibfin's repair stage looked at the latest `pending` stop.

- Category: B, Nibfin process error
- Reason: The route Job failed twice on the same step.
- Evidence: code `X`
- Done: Nothing; `pending` stays.
- Advice: Follow the steps below, and file the bug report below them.

### Steps

1. Read the failed run.
   Link: https://github.com/o/r/actions/runs/1
2. Upgrade the workflows: then rerun.
   ````sh
   gh nibfin upgrade -R o/r

   echo ```done```
   ````
3. Remove the `pending` label.

### Bug report for Nibfin

File it at https://github.com/taco3064/nibfin-core/issues/new with this title and body.

Title: Route fails on the same step

````markdown
## What happened

```text
boom
```

### Not a heading
````