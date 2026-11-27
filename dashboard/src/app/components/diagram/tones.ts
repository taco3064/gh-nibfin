import type { DiagramKind, KindTone } from './layout'

// tones are literal classes per kind tone, so Tailwind sees them and no
// inline style is needed.
const tones: Record<KindTone, { stroke: string; text: string; border: string }> = {
  primary: { stroke: 'stroke-primary', text: 'text-primary', border: 'border-primary' },
  info: { stroke: 'stroke-info', text: 'text-info', border: 'border-info' },
  warning: { stroke: 'stroke-warning', text: 'text-warning', border: 'border-warning' },
  success: { stroke: 'stroke-success', text: 'text-success', border: 'border-success' },
  muted: { stroke: 'stroke-muted-foreground', text: 'text-muted-foreground', border: 'border-muted-foreground' },
}

export const toneOf = (k: DiagramKind | undefined) => tones[k?.tone ?? 'muted']
