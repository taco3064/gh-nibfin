# Developing the Nibfin localhost Dashboard

Apply this guidance when Deliver changes the localhost Dashboard. The
Dashboard serves its packaged HTML, CSS and JavaScript from
`internal/dashboard/assets/`. Preserve the approved Issue's scope and the
[Dashboard service contract](dashboard.md).

## Design and interaction

- Start with the operator's task. Keep Runner or repository status and the
  next action visible. Put raw evidence and diagnostic details behind a
  disclosure. Use the packaged components and official Nibfin theme tokens
  for consistent spacing, focus, contrast and subtle boundaries.
- Make changed pages and flows usable on desktop and narrow touch screens.
  Include search, filters, graph or list navigation, Issue detail and
  confirmation states when affected. Preserve visible keyboard focus.
  Keep status text visible alongside any animation and respect reduced motion.
- Represent observed facts, unknown or stale data, submitted requests and
  confirmed effects distinctly. Preserve existing identity, session and
  permission checks when changing an operation's presentation.

## Review, fix and recheck

- When Chrome or Chromium is available in the assigned environment, run the
  Dashboard from the candidate source. Capture comparable desktop and mobile
  before and after views. Use deterministic fixtures for repeatable visual
  checks and mutating flows. Inspect loading, empty, stale and failed views
  relevant to the change, then fix issues and repeat the same scenario.
- Use a real, authorized repository for read-only integration checks when
  available. Verify the served revision, repository identity and loaded
  snapshot before capturing. Label each screenshot with the revision,
  viewport and whether the data is fixture or live. Keep credentials and
  session proofs out of captured evidence.
- Report what was observed, which issues were fixed and what still needs
  verification. If a candidate preview or browser is unavailable, use feasible
  targeted checks and report the missing visual evidence.

Use the existing GitHub-hosted browser workflow for the full applicable suite.
Use the workflow result for the matching revision as authoritative evidence
and leave the full suite with that workflow.
