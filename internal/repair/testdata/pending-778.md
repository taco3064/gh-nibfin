<!-- nibfin:pending -->
Nibfin stopped and needs a person.

- Code: `STAGE_BLOCKED`
- Run: https://github.com/taco3064/nibfin-core/actions/runs/46049685864

```text
Shape Author returned blocked (https://github.com/taco3064/nibfin-core/issues/778#issuecomment-7225879347):
This Issue cannot reach its Expected outcome until the execution repository's shared-runner workflow accepts a `repair` stage. That output is undelivered, has no owning Issue, and sits on a protected path. The Issue's merge sentence for #780 also needs an Owner-level decision. Two amendment requests for Scope follow.

## 1. Missing prerequisite: `repair` as a dispatchable stage (not a small amendment: it needs a new Issue, then an edge)

**Evidence**
- `.github/workflows/nibfin-shared-runner.yml` lines 30-33 and `templates/workflows/nibfin-shared-runner.yml.eta` line 32 declare `stage` as `type: choice` with `options: [route, scope, shape, deliver, review, finish]`.
- The previous Validator verified that GitHub rejects a workflow_dispatch value outside `options` with 422 "Provided value ... not in the list of allowed values" (https://github.com/vlukashov/boolean-workflow-input-issue; https://github.com/orgs/community/discussions/11795). So every hand-off to the agent would fail, and the Issue's outcome could not be produced or accepted.
- `src/workflows/core.test.ts` ("equals its render") requires `.github/workflows/` to equal the render of `templates/workflows/`. `.github/workflows/` is a protected path (R3.46), so Deliver cannot change the template without a red CI.
- User repositories still get their workflows from gh-nibfin's embedded templates until gh-nibfin #507 lands. Collaborator runners only admit Jobs from a `nibfin-shared-runner.yml` that matches the official template (Proposal R6.7).
- No open Issue owns this change:
  - gh-nibfin #508 covers `labeled:pending` routing and status;
  - nibfin-core #776 moves bash and keeps which stages exist;
  - #777 put workflow changes out of scope.

**Why it cannot be designed around with the current `options`**
- The route Job runs no agent and has no CLI login on a public execution repository (R5.70).
- The finish Job runs on a GitHub-hosted runner when the repository is public (R5.72), so it has no login either.
- Dispatching another stage name (for example `shape` with a `reason`) has two problems:
  - The run title would name the wrong stage. R5.5 requires the run title to carry the Issue number and the stage.
  - `dispatchStage` (`src/route/dispatch.ts:120-125`) skips a dispatch when a not-started run has the same `nibfinRunTitle`. A waiting repair run and a waiting real Shape run would suppress each other.
- No other Nibfin workflow is dispatchable and passes the R6.7 hook.

**Requested amendment**
- Scope creates a maintenance Issue in `taco3064/nibfin-core`, for example "Accept `repair` as a shared-runner stage". Its route is a hand-made maintenance PR, because the path is protected. It covers:
  - adding `repair` to `stage.options` in `templates/workflows/nibfin-shared-runner.yml.eta`;
  - updating the `stage` description in `templates/workflows/nibfin-job.yml.eta` if needed;
  - `npm run render:workflows` for `.github/workflows/`.
- The same change is needed in gh-nibfin's embedded `nibfin-shared-runner.yml` template, unless gh-nibfin #507 lands first. Scope can fold that into gh-nibfin #508 or open its own Issue.
- Add the edge: #778 Blocked by the new nibfin-core maintenance Issue. Add the gh-nibfin one too if Scope wants user repositories covered before this Issue is accepted.
- In #778's Inputs, replace nothing; add: "Blocked by <new Issue>: `nibfin-shared-runner.yml` accepts `stage: repair` (protected path, maintenance PR)."
- With that edge, the previous Shape's dispatch design (`stage: repair` on the existing `stage` job, whose `stage` input in `nibfin-job.yml` is `type: string`) stands. The fallback for a refused dispatch would then only cover execution repositories that have not yet rendered the new workflows.

## 2. The merge condition for #780 (needs the Owner)

- The Issue says the skill PR #780 and this Issue's PR are merged together "after the Owner confirms". It also says the skill is written and verified by the skill maintainers on their own route.
- Each PR alone is red on `src/config/skills.test.ts`: it pins `["accept","deliver","scope","shape"]` and `readBuiltInSkillNames()` discovers `skills/`. So the PRs must land as one change. #780's description offers folding the skill into this Issue's PR.
- The review stage merges on a clean review. Nothing in Nibfin observes an Owner confirmation, and a Shape step may not wait on a person unless the authority says so.
- The Shape therefore cannot keep the condition truthfully without an Issue change. Scope or the Owner chooses one replacement for the merge sentence:
  - **(a) Keep the confirmation:** "This Issue's PR includes `skills/repair/SKILL.md` byte-for-byte from #780's head at Deliver time, and #780 is closed unmerged when this PR merges. The Owner adds `pause` to this Issue before Review and removes it to confirm the merge."
  - **(b) Drop it:** the same sentence without the `pause` part, so the PR merges on a clean review.
- Either way, PR review compares the folded file with #780's current head. A difference is a problem for Deliver, so the two copies cannot drift apart unnoticed. Later skill edits go through the maintainers' own route as separate PRs.
- This rewrites a condition the Owner stated, so it is not a small amendment.

Context, not a fix item — Mechanism delta: none added or removed. No revision was written, because the result is blocked.
```

Remove the `pending` label to continue.