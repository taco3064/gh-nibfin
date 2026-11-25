# Accepting Dashboard frontend changes

Apply this section only when the diff touches the Dashboard frontend:
`dashboard/` or what it builds into `internal/dashboard/dist/`. For any other
change, ignore it. It adds to the Accept skill above.

Check these by reading the diff; a violation the Writer can fix within the
Shape is a defect:

- Colors for surfaces, text, borders and focus come from the Nibfin theme
  tokens (`--nf-` variables); the changed styles and components hard-code no
  color values.
- Components come from shadcn/ui, used directly or composed, wherever one
  fits; a custom component exists only where the library has nothing that
  fits, and does not duplicate an existing project component.
- Changed views keep a responsive layout: no fixed widths or layouts that force
  horizontal page scroll or push controls off screen at narrow widths.
- Any meaning shown by color or animation also has a text label, and motion
  respects reduced-motion settings.
- No screenshots or other review output are committed.

Contrast ratios and the look of each view are covered by the Writer's
screenshot review; do not start a browser for them.
