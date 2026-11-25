# Accepting Dashboard frontend changes

Apply this section only when the diff touches the Dashboard frontend:
`dashboard/` or what it builds into `internal/dashboard/dist/`. For any other
change, ignore it. It adds to the Accept skill above.

Check these by reading the diff; a violation the Writer can fix within the
Shape is a defect:

- Colors for surfaces, text, borders and focus come from the Nibfin theme
  tokens (`--nf-` variables); the changed styles and components hard-code no
  color values. The theme token source file that defines the `--nf-` values,
  and the bindings of the component library's variables to those tokens, are
  the only places color values belong.
- Components come from shadcn/ui, used directly or composed, wherever one
  fits; a custom component exists only where the library has nothing that
  fits, and does not duplicate an existing project component.
- Changed views keep a responsive layout: no fixed widths or layouts that force
  horizontal page scroll or push controls off screen at narrow widths.
- Any meaning shown by color or animation also has a text label, and motion
  respects reduced-motion settings.
- When the diff changes or adds `--nf-` token values, each text color stays at
  least 4.5:1, and each non-text color that carries meaning (control
  boundaries, focus indicators, meaningful graphics) at least 3:1, against the
  surface it is used on, in both dark and light themes; compute the ratios
  from the values in the diff. Decorative borders and layered surfaces are
  exempt.
- No screenshots or other review output are committed.

Judge everything above by reading the diff; do not start a browser. How each
view looks is the Writer's screenshot review. Screenshots left in the
workspace are not evidence: they may come from an earlier round or head, so do
not use them.
