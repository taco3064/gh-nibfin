import { useId } from 'react'

import { cn } from '@/lib/utils'

import {
  describeDiagram,
  layoutDiagram,
  sizes,
  titlePlate,
  type DiagramKind,
  type DiagramSpec,
  type Placement,
  type Point,
} from './layout'
import { DiagramText } from './DiagramText'
import { Stacked } from './Stacked'
import { toneOf } from './tones'

// DiagramLayout lists the drawings to try: 'responsive' (the default) tries
// left to right, then top to bottom; 'vertical' tries only top to bottom, for
// a flow too long to read across. When no drawing fits its box, the figure
// shows the stacked layout: one block per node.
export type DiagramLayout = 'responsive' | 'vertical'

export interface DiagramProps {
  spec: DiagramSpec
  // title is the figure's caption; description, when given, follows it.
  title: string
  description?: string
  // flow animates a slow dash along every edge, only without reduced motion.
  flow?: boolean
  className?: string
  // layout 'responsive' (the default) shows the left-to-right drawing where
  // it fits, else the top-to-bottom one; 'vertical' only ever shows the
  // top-to-bottom one. Either falls back to the stacked layout.
  layout?: DiagramLayout
  // readingOrder 'placement' (the default) lists the nodes in the first
  // drawing's reading order (the left-to-right one in responsive layout);
  // 'spec' lists them in the order the spec declares them.
  readingOrder?: 'placement' | 'spec'
}

// steps are the box widths a drawing may need, with literal container query
// classes that show or hide a presentation from that width up (the CSP allows
// no inline style, and Tailwind only sees literal classes).
const steps: { px: number; show: string; hide: string }[] = [
  { px: 1280, show: '@min-[1280px]:block', hide: '@min-[1280px]:hidden' },
  { px: 1216, show: '@min-[1216px]:block', hide: '@min-[1216px]:hidden' },
  { px: 1152, show: '@min-[1152px]:block', hide: '@min-[1152px]:hidden' },
  { px: 1088, show: '@min-[1088px]:block', hide: '@min-[1088px]:hidden' },
  { px: 1024, show: '@min-[1024px]:block', hide: '@min-[1024px]:hidden' },
  { px: 960, show: '@min-[960px]:block', hide: '@min-[960px]:hidden' },
  { px: 896, show: '@min-[896px]:block', hide: '@min-[896px]:hidden' },
  { px: 832, show: '@min-[832px]:block', hide: '@min-[832px]:hidden' },
  { px: 768, show: '@min-[768px]:block', hide: '@min-[768px]:hidden' },
  { px: 704, show: '@min-[704px]:block', hide: '@min-[704px]:hidden' },
  { px: 640, show: '@min-[640px]:block', hide: '@min-[640px]:hidden' },
  { px: 576, show: '@min-[576px]:block', hide: '@min-[576px]:hidden' },
  { px: 512, show: '@min-[512px]:block', hide: '@min-[512px]:hidden' },
  { px: 448, show: '@min-[448px]:block', hide: '@min-[448px]:hidden' },
  { px: 384, show: '@min-[384px]:block', hide: '@min-[384px]:hidden' },
  { px: 320, show: '@min-[320px]:block', hide: '@min-[320px]:hidden' },
]
type Step = (typeof steps)[number]

// floorOf is the box width a drawing of natural width needs so its text stays
// at about 80% of its size or more: the largest step within 80% of its width.
// It is 'any' for a drawing whose 80% is under the smallest step, and
// undefined for one whose 80% is past the largest: that drawing never shows.
function floorOf(width: number): Step | 'any' | undefined {
  const at = width * 0.8
  if (at > steps[0].px) return undefined
  return steps.find((s) => s.px <= at) ?? 'any'
}

// Candidate is a drawing to try, with the classes that show it.
interface Candidate {
  placement: Placement
  className: string
}

// choose keeps, in order of preference, the drawings that can show, and
// gives each the classes that show it only where its box is at least its
// floor and no earlier drawing fits. stacked is the classes of the stacked
// layout, shown where no drawing fits, or undefined when one always does.
function choose(placements: Placement[]): { drawings: Candidate[]; stacked?: string } {
  const drawings: Candidate[] = []
  // earlier is the smallest floor of the drawings kept so far.
  let earlier: Step | 'any' | undefined
  for (const placement of placements) {
    const floor = floorOf(placement.width)
    if (floor === undefined || earlier === 'any') continue
    if (earlier && floor !== 'any' && floor.px >= earlier.px) continue
    const show = floor === 'any' ? 'block' : `hidden ${floor.show}`
    drawings.push({ placement, className: earlier ? `${show} ${earlier.hide}` : show })
    earlier = floor
  }
  if (earlier === 'any') return { drawings }
  return { drawings, stacked: earlier ? `block ${earlier.hide}` : 'block' }
}

/**
 * A static figure of boxes, arrows and groups or lanes for the site (#456).
 * It renders without state or effects, so Astro outputs it as HTML at build
 * time. The figure is a size container (#474): of its presentations (the
 * left-to-right drawing, the top-to-bottom one and the stacked layout) it
 * shows the first that fits the box it actually has, through container
 * queries, so nothing scrolls sideways and nothing is cut off. All of them
 * are hidden from assistive technology; a visually hidden ordered list
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
  const narrow = layoutDiagram(spec, 'TB')
  const wide = layout === 'vertical' ? undefined : layoutDiagram(spec, 'LR')
  const items = describeDiagram(spec, readingOrder === 'spec' ? { order: spec.nodes.map((n) => n.id) } : (wide ?? narrow))
  const lanes = (spec.groupStyle ?? 'frames') === 'lanes'
  const kindsTitle = spec.kindsTitle ?? 'Kinds'
  const { drawings, stacked } = choose(wide ? [wide, narrow] : [narrow])
  return (
    <figure className={cn('not-content m-0 flex min-w-0 flex-col gap-3 p-0', className)} data-slot="diagram">
      <figcaption className="m-0 flex flex-col gap-1">
        <span className="font-heading text-base font-semibold text-foreground">{title}</span>
        {description && <span className="text-sm text-muted-foreground">{description}</span>}
      </figcaption>
      <div className="@container min-w-0" data-slot="diagram-box">
        {drawings.map(({ placement, className: show }) => (
          <div key={placement.direction} className={cn('min-w-0', show)} data-slot="diagram-drawing" data-direction={placement.direction}>
            <Drawing spec={spec} flow={flow} placement={placement} marker={`nf-diagram-${id}-${placement.direction.toLowerCase()}`} />
          </div>
        ))}
        {stacked !== undefined && <Stacked spec={spec} items={items} className={stacked} />}
      </div>
      {spec.kinds && spec.kinds.length > 0 && <Legend kinds={spec.kinds} title={kindsTitle} />}
      <DiagramText items={items} kindsTitle={kindsTitle} lanes={lanes} />
    </figure>
  )
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

function Drawing({ spec, flow, placement: p, marker }: { spec: DiagramSpec; flow: boolean; placement: Placement; marker: string }) {
  const s = sizes
  const kinds = new Map((spec.kinds ?? []).map((k) => [k.id, k]))
  const specNodes = new Map(spec.nodes.map((n) => [n.id, n]))
  return (
    <svg
      viewBox={`0 0 ${p.width} ${p.height}`}
      width={p.width}
      height={p.height}
      className="m-0 block h-auto max-w-full font-sans"
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
              <g key={j} data-badge={b.text}>
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
