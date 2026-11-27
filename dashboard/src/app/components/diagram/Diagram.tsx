import { useId } from 'react'

import { cn } from '@/lib/utils'

import {
  describeDiagram,
  edgeText,
  layoutDiagram,
  sizes,
  titlePlate,
  type DiagramKind,
  type DiagramSpec,
  type KindTone,
  type Placement,
  type Point,
} from './layout'

// DiagramLayout picks the drawings: 'responsive' (the default) draws left to
// right from the md breakpoint up and top to bottom below it; 'vertical'
// draws top to bottom at every width, for a flow too long to read across the
// content column.
export type DiagramLayout = 'responsive' | 'vertical'

export interface DiagramProps {
  spec: DiagramSpec
  // title is the figure's caption; description, when given, follows it.
  title: string
  description?: string
  // flow animates a slow dash along every edge, only without reduced motion.
  flow?: boolean
  className?: string
  // layout 'responsive' (the default) draws left to right from the md
  // breakpoint up and top to bottom below it; 'vertical' draws top to bottom
  // at every width.
  layout?: DiagramLayout
  // readingOrder 'placement' (the default) lists the nodes in the shown
  // drawing's reading order (the left-to-right one in responsive layout);
  // 'spec' lists them in the order the spec declares them.
  readingOrder?: 'placement' | 'spec'
}

// tones are literal classes per kind tone, so Tailwind sees them and no
// inline style is needed.
const tones: Record<KindTone, { stroke: string; text: string; border: string }> = {
  primary: { stroke: 'stroke-primary', text: 'text-primary', border: 'border-primary' },
  info: { stroke: 'stroke-info', text: 'text-info', border: 'border-info' },
  warning: { stroke: 'stroke-warning', text: 'text-warning', border: 'border-warning' },
  success: { stroke: 'stroke-success', text: 'text-success', border: 'border-success' },
  muted: { stroke: 'stroke-muted-foreground', text: 'text-muted-foreground', border: 'border-muted-foreground' },
}
const toneOf = (k: DiagramKind | undefined) => tones[k?.tone ?? 'muted']

/**
 * A static figure of boxes, arrows and groups or lanes for the site (#456).
 * It renders without state or effects, so Astro outputs it as HTML at build
 * time: a left-to-right drawing from the md breakpoint up and a top-to-bottom
 * one below it (with layout="vertical", the top-to-bottom one at every width),
 * hidden from assistive technology, and a visually hidden ordered list that
 * carries the same information at every width.
 */
export function Diagram({
  spec,
  title,
  description,
  flow = false,
  className,
  layout = 'responsive',
  readingOrder = 'placement',
}: DiagramProps) {
  const id = useId().replace(/[^A-Za-z0-9_-]/g, '')
  const vertical = layout === 'vertical'
  const narrow = layoutDiagram(spec, 'TB')
  const wide = vertical ? undefined : layoutDiagram(spec, 'LR')
  const items = describeDiagram(spec, readingOrder === 'spec' ? { order: spec.nodes.map((n) => n.id) } : (wide ?? narrow))
  const lanes = (spec.groupStyle ?? 'frames') === 'lanes'
  const kindsTitle = spec.kindsTitle ?? 'Kinds'
  return (
    <figure className={cn('not-content m-0 flex min-w-0 flex-col gap-3 p-0', className)} data-slot="diagram">
      <figcaption className="m-0 flex flex-col gap-1">
        <span className="font-heading text-base font-semibold text-foreground">{title}</span>
        {description && <span className="text-sm text-muted-foreground">{description}</span>}
      </figcaption>
      {wide ? (
        <>
          <div className="hidden max-w-full overflow-x-auto md:block" data-slot="diagram-wide">
            <Drawing spec={spec} flow={flow} placement={wide} marker={`nf-diagram-${id}-lr`} />
          </div>
          <div className="max-w-full overflow-x-auto md:hidden" data-slot="diagram-narrow">
            <Drawing spec={spec} flow={flow} placement={narrow} marker={`nf-diagram-${id}-tb`} />
          </div>
        </>
      ) : (
        <div className="max-w-full overflow-x-auto" data-slot="diagram-vertical">
          <Drawing spec={spec} flow={flow} placement={narrow} marker={`nf-diagram-${id}-tb`} />
        </div>
      )}
      {spec.kinds && spec.kinds.length > 0 && <Legend kinds={spec.kinds} title={kindsTitle} />}
      <ol className="sr-only">
        {items.map((item) => (
          <li key={item.id}>
            {sentence(item.detail ? `${item.title}: ${item.detail}` : item.title)}
            {item.badges.length > 0 && ` Labels: ${item.badges.join(', ')}.`}
            {item.kinds && ` ${kindsTitle}: ${item.kinds.join('; ')}.`}
            {item.group && ` ${lanes ? 'Lane' : 'Group'}: ${item.group}.`}
            {item.edges.length > 0 && ` Leads ${item.edges.map(edgeText).join(', ')}.`}
          </li>
        ))}
      </ol>
    </figure>
  )
}

// sentence ends text with a full stop unless it already ends with one.
function sentence(text: string): string {
  return /[.!?]$/.test(text) ? text : `${text}.`
}

// Legend explains each kind: its tone and icon, its title and description.
function Legend({ kinds, title }: { kinds: DiagramKind[]; title: string }) {
  return (
    <div className="flex flex-col gap-2" data-slot="diagram-legend">
      <span className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</span>
      <ul className="grid gap-3 sm:grid-cols-2">
        {kinds.map((k) => {
          const tone = toneOf(k)
          const Icon = k.icon
          return (
            <li key={k.id} className="flex items-start gap-3" data-kind={k.id}>
              <span
                aria-hidden="true"
                className={cn('flex size-8 shrink-0 items-center justify-center rounded-md border-2 bg-card', tone.border)}
              >
                {Icon && <Icon className={cn('size-4', tone.text)} />}
              </span>
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="text-sm font-semibold text-foreground">{k.title}</span>
                {k.description && <span className="text-sm text-muted-foreground">{k.description}</span>}
              </span>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

// path draws a polyline whose corners are rounded.
function path(points: Point[]): string {
  const f = (v: number) => Math.round(v * 100) / 100
  let d = `M${f(points[0].x)},${f(points[0].y)}`
  for (let i = 1; i < points.length - 1; i++) {
    const [a, b, c] = [points[i - 1], points[i], points[i + 1]]
    const lin = Math.hypot(b.x - a.x, b.y - a.y)
    const lout = Math.hypot(c.x - b.x, c.y - b.y)
    const r = Math.min(8, lin / 2, lout / 2)
    if (r < 0.5) {
      d += ` L${f(b.x)},${f(b.y)}`
      continue
    }
    const p = { x: b.x + ((a.x - b.x) * r) / lin, y: b.y + ((a.y - b.y) * r) / lin }
    const q = { x: b.x + ((c.x - b.x) * r) / lout, y: b.y + ((c.y - b.y) * r) / lout }
    d += ` L${f(p.x)},${f(p.y)} Q${f(b.x)},${f(b.y)} ${f(q.x)},${f(q.y)}`
  }
  const last = points[points.length - 1]
  return `${d} L${f(last.x)},${f(last.y)}`
}

// floors are the minimum widths a drawing may shrink to, as literal classes
// (the CSP allows no inline style). Past its floor a drawing scrolls inside
// its figure, so its text stays at least about 80% of its natural size.
const floors: [number, string][] = [
  [1280, 'min-w-[80rem]'],
  [1216, 'min-w-[76rem]'],
  [1152, 'min-w-[72rem]'],
  [1088, 'min-w-[68rem]'],
  [1024, 'min-w-[64rem]'],
  [960, 'min-w-[60rem]'],
  [896, 'min-w-[56rem]'],
  [832, 'min-w-[52rem]'],
  [768, 'min-w-[48rem]'],
  [704, 'min-w-[44rem]'],
  [640, 'min-w-[40rem]'],
  [576, 'min-w-[36rem]'],
  [512, 'min-w-[32rem]'],
  [448, 'min-w-[28rem]'],
  [384, 'min-w-96'],
  [320, 'min-w-80'],
]

// minWidth picks the largest floor within 80% of the natural width.
function minWidth(width: number): string | undefined {
  return floors.find(([px]) => px <= width * 0.8)?.[1]
}

function Drawing({ spec, flow, placement: p, marker }: { spec: DiagramSpec; flow: boolean; placement: Placement; marker: string }) {
  const s = sizes
  const kinds = new Map((spec.kinds ?? []).map((k) => [k.id, k]))
  const specNodes = new Map(spec.nodes.map((n) => [n.id, n]))
  return (
    <svg
      viewBox={`0 0 ${p.width} ${p.height}`}
      width={p.width}
      height={p.height}
      className={cn('m-0 block h-auto max-w-full font-sans', minWidth(p.width))}
      aria-hidden="true"
      data-direction={p.direction}
    >
      <defs>
        <marker
          id={marker}
          viewBox="0 0 10 10"
          refX="10"
          refY="5"
          markerWidth="6"
          markerHeight="6"
          orient="auto-start-reverse"
        >
          <path d="M0,0 L10,5 L0,10 z" className="fill-muted-foreground" />
        </marker>
      </defs>
      {p.groups.map((g, i) => (
        <g key={g.id ?? `lane-${i}`} data-group={g.id ?? ''}>
          {p.groupStyle === 'lanes' ? (
            <rect x={g.x} y={g.y} width={g.width} height={g.height} rx={10} className="fill-muted/40 stroke-border-subtle" />
          ) : (
            <rect
              x={g.x}
              y={g.y}
              width={g.width}
              height={g.height}
              rx={12}
              strokeWidth={1.5}
              strokeDasharray="6 4"
              className="fill-none stroke-border"
            />
          )}
        </g>
      ))}
      {p.edges.map((e, i) => (
        <path
          key={`e${i}`}
          d={path(e.points)}
          fill="none"
          strokeWidth={1.5}
          markerEnd={`url(#${marker})`}
          className="stroke-muted-foreground"
        />
      ))}
      {/* The flow: a slow dash over each edge, shown and moving only without
          reduced motion, so a reduced-motion figure is the static one. */}
      {flow &&
        p.edges.map((e, i) => (
          <path
            key={`f${i}`}
            d={path(e.points)}
            fill="none"
            strokeWidth={2}
            strokeLinecap="round"
            strokeDasharray="2 14"
            className="hidden stroke-primary motion-safe:inline motion-safe:animate-diagram-flow"
            data-slot="diagram-flow"
          />
        ))}
      {/* Group titles go over the edges; a frame's title sits on a plate so
          an edge entering the frame does not cross its text. */}
      {p.groups.map((g, i) =>
        g.titleLines.map((t) => (
          <g key={`t${i}-${t.y}`}>
            {p.groupStyle === 'frames' && <rect {...titlePlate(t)} rx={3} className="fill-background" />}
            <text x={t.x} y={t.y} fontSize={s.groupTitleSize} fontWeight={600} className="fill-muted-foreground font-heading">
              {t.text}
            </text>
          </g>
        )),
      )}
      {p.edges.map(
        (e, i) =>
          e.label && (
            <g key={`l${i}`}>
              <rect
                x={e.label.x}
                y={e.label.y}
                width={e.label.width}
                height={e.label.height}
                rx={4}
                className="fill-background stroke-border-subtle"
              />
              {e.label.lines.map((line, j) => (
                <text
                  key={j}
                  x={e.label!.x + e.label!.width / 2}
                  y={e.label!.y + s.labelPad / 2 + (j + 1) * s.labelLine - 4}
                  fontSize={s.labelSize}
                  textAnchor="middle"
                  className="fill-foreground"
                >
                  {line}
                </text>
              ))}
            </g>
          ),
      )}
      {p.nodes.map((n) => {
        const node = specNodes.get(n.id)!
        const first = node.kinds?.length ? kinds.get(node.kinds[0]) : undefined
        const Icon = node.icon
        return (
          <g key={n.id} transform={`translate(${n.x} ${n.y})`} data-node={n.id}>
            <rect
              width={n.width}
              height={n.height}
              rx={10}
              strokeWidth={first ? 1.5 : undefined}
              className={cn('fill-card', first ? toneOf(first).stroke : 'stroke-border')}
            />
            {Icon && n.icon && (
              <g transform={`translate(${n.icon.x} ${n.icon.y})`}>
                <Icon size={n.icon.width} aria-hidden="true" className={first ? toneOf(first).text : 'text-primary'} />
              </g>
            )}
            {n.lines.map((l, j) => (
              <text
                key={j}
                x={l.x}
                y={l.y}
                fontSize={l.kind === 'title' ? s.titleSize : s.detailSize}
                fontWeight={l.kind === 'title' ? 600 : 400}
                className={l.kind === 'title' ? 'fill-card-foreground font-heading' : 'fill-muted-foreground'}
              >
                {l.text}
              </text>
            ))}
            {n.badges.map((b, j) => (
              <g key={j}>
                <rect x={b.x} y={b.y} width={b.width} height={b.height} rx={b.height / 2} className="fill-none stroke-border" />
                <text
                  x={b.x + b.width / 2}
                  y={b.y + b.height / 2}
                  fontSize={s.badgeSize}
                  textAnchor="middle"
                  dominantBaseline="central"
                  className="fill-card-foreground font-mono"
                >
                  {b.text}
                </text>
              </g>
            ))}
            {n.markers.map((m) => {
              const k = kinds.get(m.kind)!
              const tone = toneOf(k)
              const KindIcon = k.icon
              return (
                <g key={m.kind} data-marker={m.kind}>
                  {KindIcon ? (
                    <g transform={`translate(${m.icon.x} ${m.icon.y})`}>
                      <KindIcon size={m.icon.width} aria-hidden="true" className={tone.text} />
                    </g>
                  ) : (
                    <circle
                      cx={m.icon.x + m.icon.width / 2}
                      cy={m.icon.y + m.icon.height / 2}
                      r={m.icon.width / 3}
                      className={cn('fill-none', tone.stroke)}
                      strokeWidth={2}
                    />
                  )}
                  {m.lines.map((l, j) => (
                    <text key={j} x={l.x} y={l.y} fontSize={s.markerSize} className="fill-card-foreground">
                      {l.text}
                    </text>
                  ))}
                </g>
              )
            })}
          </g>
        )
      })}
    </svg>
  )
}
