import { ArrowRight } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

import { toneOf } from './tones'
import type { DescribedNode, DiagramSpec } from './layout'

/**
 * The stacked presentation of a diagram (#474): one block per node, top to
 * bottom, for a box too narrow for any drawing. It shows what the drawing
 * shows (title, detail, badges, kind markers, group or lane, and every
 * hand-off, paths back included) from the same described items as the text
 * list, so it reads in the same order. Only the blocks are drawn: there is no
 * lane background. It is hidden from assistive technology like the drawings;
 * the figure's visually hidden list stays its single text alternative.
 */
export function Stacked({ spec, items, className }: { spec: DiagramSpec; items: DescribedNode[]; className: string }) {
  const nodes = new Map(spec.nodes.map((n) => [n.id, n]))
  const kinds = new Map((spec.kinds ?? []).map((k) => [k.id, k]))
  return (
    <div aria-hidden="true" className={className} data-slot="diagram-stacked">
      <div className="flex flex-col gap-2">
        {items.map((item) => {
          const node = nodes.get(item.id)!
          const marks = (node.kinds ?? []).map((id) => kinds.get(id)!)
          const tone = marks.length ? toneOf(marks[0]) : undefined
          const Icon = node.icon
          return (
            <Card
              key={item.id}
              className={cn('min-w-0 gap-2 rounded-lg p-3 shadow-none', marks.length ? 'border-[1.5px]' : '', tone ? tone.border : 'border-border')}
              data-block={item.id}
            >
              <div className="flex flex-wrap items-start justify-between gap-x-3 gap-y-1">
                <span className="flex min-w-0 items-start gap-2">
                  {Icon && <Icon aria-hidden="true" className={cn('mt-0.5 size-4 shrink-0', tone ? tone.text : 'text-primary')} />}
                  <span className="font-heading text-sm font-semibold">{item.title}</span>
                </span>
                {item.group && (
                  <Badge variant="outline" className="border-border-subtle bg-muted/40 font-normal text-muted-foreground" data-slot="diagram-stacked-group">
                    {item.group}
                  </Badge>
                )}
              </div>
              {item.detail && <span className="text-sm text-muted-foreground">{item.detail}</span>}
              {item.badges.length > 0 && (
                <span className="flex flex-wrap gap-1">
                  {item.badges.map((b) => (
                    <Badge key={b} variant="outline" className="py-0 font-mono font-normal">
                      {b}
                    </Badge>
                  ))}
                </span>
              )}
              {marks.length > 0 && (
                <span className="flex flex-wrap gap-x-3 gap-y-1 text-xs">
                  {marks.map((k) => {
                    const KindIcon = k.icon
                    return (
                      <span key={k.id} className="flex items-center gap-1">
                        {KindIcon ? (
                          <KindIcon aria-hidden="true" className={cn('size-3 shrink-0', toneOf(k).text)} />
                        ) : (
                          <span className={cn('size-2.5 shrink-0 rounded-full border-2', toneOf(k).border)} />
                        )}
                        {k.title}
                      </span>
                    )
                  })}
                </span>
              )}
              {item.edges.length > 0 && (
                <ul className="m-0 flex list-none flex-col gap-1 p-0 text-sm" data-slot="diagram-stacked-edges">
                  {item.edges.map((e, i) => (
                    <li key={i} className="m-0 flex items-start gap-1.5">
                      <ArrowRight aria-hidden="true" className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                      <span className="min-w-0">
                        {e.to}
                        {e.label && <span className="text-muted-foreground"> ({e.label})</span>}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </Card>
          )
        })}
      </div>
    </div>
  )
}
