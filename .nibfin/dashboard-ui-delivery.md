# Developing the Dashboard frontend

Apply this section only when the change touches the Dashboard frontend:
`dashboard/` or what it builds into `internal/dashboard/dist/`. For any other
change, ignore it. It adds to the Deliver skill above and keeps the approved
Issue's scope and checks.

## Design

- Optimize for readability and a clean, uncluttered look. Lead each view with
  what the operator needs to see or do; put raw detail behind a disclosure.
- Responsive layout is required. Every changed view works from a narrow phone
  width to a wide desktop without horizontal page scroll, clipped text or
  off-screen controls.
- Prefer graphics over plain text: status badges, progress, charts and graphs
  where they carry the meaning (for example the Issue dependency graph). Keep a
  text label beside any color or animation, and respect reduced motion.
- Build from shadcn/ui components first, used directly or composed. Write a
  custom component only when the library has nothing that fits, and reuse an
  existing project component for the same behavior.
- Before designing a new layout or component, look at external references
  (well-designed web pages, the shadcn/ui examples and blocks, or an available
  design skill), learn how they solve the same problem, and adapt the good
  parts. Name the references used in the handoff.
- Use the Nibfin theme tokens (`--nf-` variables) and fonts for surfaces, text,
  borders and focus; do not hard-code colors. Keep text contrast at least
  4.5:1 and non-text contrast at least 3:1 in both dark and light themes, and
  keep visible keyboard focus.

## Screenshot review before each commit

Every commit that changes how the Dashboard looks needs a browser review first:

1. In `dashboard/`, run `npm ci`, then `npx playwright install --only-shell
   chromium` (the lockfile decides the Playwright version; Core keeps the
   browser in the tool cache).
2. Build and serve the candidate, then run the project's screenshot script
   for every changed view at several widths, at least 375, 768, 1280 and
   1920 px, in dark and light themes where the change affects color.
3. Look at every screenshot. Check layout, wrapping, overflow, alignment,
   contrast, empty, loading and failed states, and the open state of changed
   dialogs and menus. Fix what is wrong and repeat the same captures.
4. Keep screenshots under `/tmp`, never in the repository, and keep session
   proofs and tokens out of them.

In the handoff, list the views, widths and themes reviewed, what was fixed
after review, and anything not verified. If the browser cannot run, say so
and do not claim a visual review.

Run only the frontend checks related to the change. CI runs the full build
and test suite and is the authoritative result.
