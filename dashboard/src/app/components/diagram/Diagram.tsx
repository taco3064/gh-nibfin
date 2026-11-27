import { useId } from 'react'

import { cn } from '@/lib/utils'

import {
  describeDiagram,
  edgeText,
  layoutDiagram,
  sizes,
  titlePlate,
  type DiagramSpec,
  type Placement,
  type Point,
} from './layout'

export interface DiagramProps {
  spec: DiagramSpec
  // title is the figure's caption; description, when given, follows it.
  title: string
  description?: string
  className?: string
}

/**
 * A static figure of boxes, arrows and groups or lanes for the site (#456).
 * It renders without state or effects, so Astro outputs it as HTML at build
 * time: a left-to-right drawing from the md breakpoint up and a top-to-bottom
 * one below it, both hidden from assistive technology, and a visually hidden
 * ordered list that carries the same information at every width.
 */
export function Diagram({ spec, title, description, className }: DiagramProps) {
  const id = useId().replace(/[^A-Za-z0-9_-]/g, '')
  const wide = layoutDiagram(spec, 'LR')
  const narrow = layoutDiagram(spec, 'TB')
  const items = describeDiagram(spec, wide)
  const lanes = (spec.groupStyle ?? 'frames') === 'lanes'
  return (
    <figure className={cn('not-content m-0 flex min-w-0 flex-col gap-3 p-0', className)} data-slot="diagram">
      <figcaption className="m-0 flex flex-col gap-1">
        <span className="font-heading text-base font-semibold text-foreground">{title}</span>
        {description && <span className="text-sm text-muted-foreground">{description}</span>}
      </figcaption>
      <div className="hidden max-w-full overflow-x-auto md:block" data-slot="diagram-wide">
        <Drawing placement={wide} marker={`nf-diagram-${id}-lr`} />
      </div>
      <div className="max-w-full overflow-x-auto md:hidden" data-slot="diagram-narrow">
        <Drawing placement={narrow} marker={`nf-diagram-${id}-tb`} />
      </div>
      <ol className="sr-only">
        {items.map((item) => (
          <li key={item.id}>
            {item.title}
            {item.detail && `: ${item.detail}`}.{item.badges.length > 0 && ` Labels: ${item.badges.join(', ')}.`}
            {item.group && ` ${lanes ? 'Lane' : 'Group'}: ${item.group}.`}
            {item.edges.length > 0 && ` Leads ${item.edges.map(edgeText).join(', ')}.`}
          </li>
        ))}
      </ol>
    </figure>
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

function Drawing({ placement: p, marker }: { placement: Placement; marker: string }) {
  const s = sizes
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
      {p.nodes.map((n) => (
        <g key={n.id} transform={`translate(${n.x} ${n.y})`} data-node={n.id}>
          <rect width={n.width} height={n.height} rx={10} className="fill-card stroke-border" />
          {n.lines.map((l, j) => (
            <text
              key={j}
              x={s.nodePadX}
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
        </g>
      ))}
    </svg>
  )
}
