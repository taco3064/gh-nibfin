import type { ActionLabels } from '@/components/operation/operation-dialog'
import type { Values } from '@/lib/operation'

import { optionKey, replyKey } from './use-repair-target'

// The Repair dialog answers a choose record with exactly one of an option
// and a reply in the person's own words (Proposal R4.51, R9.54): the
// request carries the option while the reply is blank, and only the reply
// once it has text. It never carries neither, which would remove pending
// with no decision and rerun the Issue.

// replying reports whether the values send a reply: its text is not blank.
export function replying(values: Values): boolean {
  const v = values[replyKey]
  return typeof v === 'string' && v.trim() !== ''
}

// pickOption is the change of choosing option n: it becomes the option and
// the reply is cleared.
export function pickOption(n: string): Values {
  return { [optionKey]: n, [replyKey]: '' }
}

// typeReply is the change of typing text into the reply, where last is the
// option picked last. A reply with text clears the option and remembers
// it; a blank reply brings it back.
export function typeReply(text: string, values: Values, last: string): { changes: Values; last: string } {
  if (text.trim() === '') return { changes: { [replyKey]: text, [optionKey]: last }, last }
  const current = values[optionKey]
  const keep = typeof current === 'string' && current !== '' ? current : last
  return { changes: { [replyKey]: text, [optionKey]: '' }, last: keep }
}

// repairLabels are the dialog's buttons: an option is sent at once, as
// resume is; a reply stops at the preview of the exact comment first.
export function repairLabels(values: Values): ActionLabels {
  return replying(values) ? { submit: 'Preview reply', apply: 'Send reply' } : {}
}
