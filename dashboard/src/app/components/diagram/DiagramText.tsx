import { edgeText, type DescribedNode } from './layout'

export interface DiagramTextProps {
  // items are the described nodes, in reading order.
  items: DescribedNode[]
  // kindsTitle names the kinds of an item that has any.
  kindsTitle?: string
  // lanes says the groups are lanes, so an item reads "Lane:", not "Group:".
  lanes?: boolean
}

/**
 * The text alternative of a site figure: a visually hidden ordered list with
 * every node once, its labels, kinds, group or lane and hand-offs. Diagram
 * renders it, and so does a figure with its own presentation (#476), so both
 * read the same way.
 */
export function DiagramText({ items, kindsTitle = 'Kinds', lanes = false }: DiagramTextProps) {
  return (
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
  )
}

// sentence ends text with a full stop unless it already ends with one.
function sentence(text: string): string {
  return /[.!?]$/.test(text) ? text : `${text}.`
}
