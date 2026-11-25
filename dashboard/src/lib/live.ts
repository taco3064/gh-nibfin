// Live updates: the page's side of GET /api/live. The server streams
// Server-Sent Events frames; EventSource cannot send the session header and
// the session never goes in a URL, so the stream is read with fetch.

import { currentSession, type ApiError } from '@/lib/session'
import type { PersonView, StatusIssue } from '@/lib/status'

// LiveEvent is one event of the stream. at is the server's RFC 3339 time.
// partial comes before the first snapshot only: the part of the first read
// done so far, with what is still loading.
export type LiveEvent =
  | { event: 'partial'; at: string; result: unknown; loading: string[] }
  | { event: 'snapshot'; at: string; result: PersonView }
  | { event: 'issue'; at: string; repo: string; issue: StatusIssue }
  | { event: 'checked'; at: string }
  | { event: 'podman'; watching: boolean; error?: ApiError }
  | { event: 'error'; source: 'github' | 'status'; error: ApiError }

// LiveEnd is why the stream ended: the request failed (network), the server
// refused it (http), the stream closed (closed) or the page aborted it.
export interface LiveEnd {
  reason: 'network' | 'http' | 'closed' | 'aborted'
  error: ApiError
}

// parseEvents takes the complete events from buffer and returns their data
// and the text after them, which a later chunk completes. data: lines
// append to the event's data, a blank line dispatches it, and lines
// starting with ':' are comments.
export function parseEvents(buffer: string): { events: string[]; rest: string } {
  const events: string[] = []
  let data: string[] = []
  let start = 0
  let pos = 0
  for (;;) {
    const nl = buffer.indexOf('\n', pos)
    if (nl < 0) break
    let line = buffer.slice(pos, nl)
    if (line.endsWith('\r')) line = line.slice(0, -1)
    pos = nl + 1
    if (line === '') {
      if (data.length > 0) events.push(data.join('\n'))
      data = []
      start = pos
      continue
    }
    if (line.startsWith(':')) continue
    const colon = line.indexOf(':')
    const field = colon < 0 ? line : line.slice(0, colon)
    let value = colon < 0 ? '' : line.slice(colon + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'data') data.push(value)
  }
  return { events, rest: buffer.slice(start) }
}

type Fetch = typeof fetch

// openLive reads the stream until it ends and calls onEvent with each
// event. It resolves with why the stream ended.
export async function openLive(onEvent: (e: LiveEvent) => void, signal: AbortSignal, fetcher: Fetch = fetch): Promise<LiveEnd> {
  const session = currentSession()
  if (session === null) return { reason: 'http', error: { code: 'session_required', message: 'no session is open' } }
  const aborted = (): LiveEnd => ({ reason: 'aborted', error: { code: 'cancelled', message: 'the page closed the live stream' } })
  let res: Response
  try {
    res = await fetcher('/api/live', { headers: { 'X-Nibfin-Session': session }, signal })
  } catch (e) {
    if (signal.aborted) return aborted()
    return { reason: 'network', error: { code: 'network_error', message: `the request to /api/live failed: ${String(e)}` } }
  }
  if (!res.ok || !res.body) {
    let error: Partial<ApiError> | undefined
    try {
      error = ((await res.json()) as { error?: Partial<ApiError> } | null)?.error
    } catch {
      // A body that is not JSON keeps the status as the message.
    }
    return { reason: 'http', error: { code: error?.code ?? 'error', message: error?.message ?? `/api/live answered ${res.status}` } }
  }
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
  let buffer = ''
  // lastError is the error event the server sent last before closing.
  let lastError: ApiError | undefined
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      const parsed = parseEvents(buffer + value)
      buffer = parsed.rest
      for (const data of parsed.events) {
        let e: LiveEvent
        try {
          e = JSON.parse(data) as LiveEvent
        } catch {
          continue
        }
        if (e.event === 'error' && e.source === 'status') lastError = e.error
        else if (e.event === 'snapshot' || e.event === 'issue') lastError = undefined
        onEvent(e)
      }
    }
  } catch (e) {
    if (signal.aborted) return aborted()
    return { reason: 'network', error: { code: 'network_error', message: `the live stream broke: ${String(e)}` } }
  }
  if (signal.aborted) return aborted()
  return { reason: 'closed', error: lastError ?? { code: 'stream_closed', message: 'the live stream ended' } }
}
