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
  parts. Name the references used in your final result report.
- Use the Nibfin theme tokens (`--nf-` variables) and fonts for surfaces, text,
  borders and focus; do not hard-code colors. The only exceptions are the theme
  token source file that defines the `--nf-` values and the bindings of the
  component library's variables to those tokens. Keep text contrast at least
  4.5:1, and at least 3:1 for non-text parts that carry meaning (control
  boundaries, focus indicators, meaningful graphics), in both dark and light
  themes; decorative borders and layered surfaces are exempt. Keep visible
  keyboard focus.

## Screenshot review before each visual commit

Every commit that changes how the Dashboard looks gets a browser review first.
The review starts with the first such commit after the project has its
screenshot script and a Playwright version pinned in `dashboard/`'s lockfile;
the Issue that creates them reviews its views once they exist.

1. In `dashboard/`, run `npm ci`, then `npx playwright install --only-shell
   chromium`. The lockfile decides the Playwright version. Leave
   `PLAYWRIGHT_BROWSERS_PATH` as Core sets it: the browser lives in Core's
   tool cache, outside the repository.
2. Run the project's screenshot script for every changed view at several
   widths, at least 375, 768, 1280 and 1920 px, in dark and light themes where
   the change affects color. The script builds, serves and stops the candidate
   itself within one foreground command (for example Playwright's `webServer`
   setting); never start a server in the background. The Issue that creates
   the script writes it that way.
3. Look at every screenshot. Check layout, wrapping, overflow, alignment,
   contrast, empty, loading and failed states, and the open state of changed
   dialogs and menus. Fix what is wrong and repeat the same captures.
4. Write screenshots to an ignored folder in the repository (for example
   `dashboard/.screenshots/`, with a narrow `.gitignore` entry); never commit
   them, and keep session proofs and tokens out of them.

In your final result report, list the views, widths and themes reviewed, what
was fixed after review, and anything not verified.

A screenshot script this change creates or modifies must build, serve and
capture successfully before you return done; if it does not, that is a defect
to fix. Only when the environment stops the review — the browser cannot be
installed or started, or an existing script this change does not touch cannot
serve the candidate on its own — judge the views from the code, still commit
the change and return done: report the exact error, say that the visual review
is unverified, and do not claim it.

## Checks

Once `dashboard/package-lock.json` exists, the repository's CI runs the
Dashboard checks on Linux and Windows with Node 24, in `dashboard/` and before
the Go steps: `npm ci`, then `npm run build`, `npm run typecheck`,
`npm run lint` and `npm test`.

These four scripts are the contract `dashboard/` keeps: the Issue that creates
`dashboard/` adds all four, and later changes keep them. `npm test` runs unit
tests only and starts no browser; the screenshot review above is separate.

Run only the checks related to your change and leave the rest to CI, whose
failure comes back to you as a `ci-failure` fix.
