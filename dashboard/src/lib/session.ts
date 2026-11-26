// Session client: the page's side of the #333 session contract. The proof in
// the URL fragment is exchanged once for the session, which stays in this
// module's memory only and goes with every API call.

export interface ApiError {
  code: string
  message: string
}

export type Outcome<T> = { ok: true; value: T } | { ok: false; error: ApiError }

type Fetch = typeof fetch

const proofPattern = /^#proof=([A-Z2-7]+)$/

// readProof returns the proof in hash, or null. On a match it removes the
// fragment from the address bar before any request is made.
export function readProof(hash: string, history?: Pick<History, 'replaceState'>): string | null {
  const m = proofPattern.exec(hash)
  if (!m) return null
  history?.replaceState(null, '', location.pathname + location.search)
  return m[1]
}

let session: string | null = null
let exchange: Promise<Outcome<string>> | null = null

// openSession exchanges proof for the session. It runs at most once per page
// load: a repeated call gets the first result, so the single-use proof is
// never sent twice.
export function openSession(proof: string, fetcher: Fetch = fetch): Promise<Outcome<string>> {
  exchange ??= post<{ session?: string }>(fetcher, '/api/session', { 'X-Nibfin-Proof': proof }).then(
    (r) => {
      if (!r.ok) return r
      if (!r.value.session) return failure('error', 'the session exchange returned no session')
      session = r.value.session
      return { ok: true, value: session }
    },
  )
  return exchange
}

// api posts body as JSON to path with the session.
export function api<T>(path: string, body: unknown, fetcher: Fetch = fetch): Promise<Outcome<T>> {
  if (session === null) return Promise.resolve(failure('session_required', 'no session is open'))
  return post<T>(fetcher, path, { 'X-Nibfin-Session': session, 'Content-Type': 'application/json' }, JSON.stringify(body))
}

// apiGet reads path with the session.
export async function apiGet<T>(path: string, fetcher: Fetch = fetch): Promise<Outcome<T>> {
  if (session === null) return failure('session_required', 'no session is open')
  return (await send<T>(fetcher, path, { method: 'GET', headers: { 'X-Nibfin-Session': session } })).outcome
}

// PlanOutcome is the answer of a plan route. A failure keeps the plan and the
// result its error body carries: a read-only plan whose read-back failed
// answers 422 with both.
export type PlanOutcome<T> =
  | { ok: true; value: T }
  | { ok: false; error: ApiError; status?: number; plan?: unknown; result?: unknown }

// planRequest posts body as JSON to a plan route with the session.
export async function planRequest<T>(path: string, body: unknown, fetcher: Fetch = fetch): Promise<PlanOutcome<T>> {
  if (session === null) return failure('session_required', 'no session is open')
  const r = await send<T>(fetcher, path, {
    method: 'POST',
    headers: { 'X-Nibfin-Session': session, 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (r.outcome.ok) return r.outcome
  const out: PlanOutcome<T> = { ...r.outcome }
  if (r.status !== undefined) out.status = r.status
  const extra = r.parsed as { plan?: unknown; result?: unknown } | null | undefined
  if (extra && typeof extra === 'object') {
    if (extra.plan !== undefined) out.plan = extra.plan
    if (extra.result !== undefined) out.result = extra.result
  }
  return out
}

// ApplyOutcome is the end of an apply. unknown is set when the request failed
// before the final line arrived: the write may or may not have happened.
export type ApplyOutcome<T> = { ok: true; value: T } | { ok: false; error: ApiError; unknown?: boolean; status?: number }

// StreamOutcome is ApplyOutcome with the result an error line carries when
// the read-back returned one.
export type StreamOutcome<T> =
  | { ok: true; value: T }
  | { ok: false; error: ApiError; unknown?: boolean; status?: number; result?: T }

// StreamLine is one parsed NDJSON line: of an apply, progress, login_step,
// result or error; of the status read, partial (with loading), result or
// error; of the planning stream, checking (with check), checked (with
// steps), planned (with id and plan), result (with plan) or error (with
// plan).
export interface StreamLine {
  event?: string
  progress?: unknown
  step?: unknown
  loading?: unknown
  result?: unknown
  error?: Partial<ApiError>
  check?: unknown
  steps?: unknown
  id?: unknown
  plan?: unknown
}

// planStream plans command through the planning stream route. Each
// checking and checked line goes to onEvent as it arrives; the final line
// is the outcome, shaped as planRequest's: planned and result lines are the
// plan route's body, and an error line keeps the plan and the result it
// carries. A non-2xx answer before the stream is a failure with its status.
// An aborted signal ends the request, which cancels the plan on the server.
export async function planStream<T>(
  command: string,
  body: unknown,
  onEvent: (line: StreamLine) => void,
  fetcher: Fetch = fetch,
  signal?: AbortSignal,
): Promise<PlanOutcome<T>> {
  let final: StreamLine | null = null
  const o = await postStream<unknown>(
    `/api/commands/${encodeURIComponent(command)}/plan/stream`,
    body,
    'the plan failed',
    (line) => {
      if (line.event === 'planned' || line.event === 'result' || line.event === 'error') final = line
      else onEvent(line)
    },
    fetcher,
    signal,
  )
  const last = final as StreamLine | null
  if (last && (last.event === 'planned' || last.event === 'result')) {
    const { event: _event, ...value } = last
    return { ok: true, value: value as T }
  }
  if (last && last.event === 'error') {
    const out: PlanOutcome<T> = failure(last.error?.code ?? 'error', last.error?.message ?? 'the plan failed')
    if (last.plan !== undefined && last.plan !== null) out.plan = last.plan
    if (last.result !== undefined && last.result !== null) out.result = last.result
    return out
  }
  if (o.ok) return failure('stream_incomplete', 'the planning stream ended without a plan')
  const out: PlanOutcome<T> = { ok: false, error: o.error }
  if (o.status !== undefined) out.status = o.status
  return out
}

// applyStream applies the stored plan id and reads its NDJSON stream as it
// arrives, handing every complete line to onEvent. The last result or error
// line is the outcome; a stream that ends or breaks without one is unknown,
// and a non-2xx answer before the stream is a failure with its status. An
// aborted signal ends the request, which cancels the apply on the server;
// it comes out as unknown.
export function applyStream<T>(
  id: string,
  confirm: boolean,
  onEvent: (line: StreamLine) => void,
  fetcher: Fetch = fetch,
  signal?: AbortSignal,
): Promise<StreamOutcome<T>> {
  return postStream<T>(`/api/plans/${encodeURIComponent(id)}/apply`, { confirm }, 'the apply failed', onEvent, fetcher, signal)
}

// readStatus reads status through the status read route, the repository
// view of repo or, with repo empty, the person view. Each partial line goes
// to onPartial with its result and the sections still loading; the result
// line is the outcome. An aborted signal cancels the read on the server.
export function readStatus<T>(
  repo: string,
  onPartial: (result: unknown, loading: string[]) => void,
  fetcher: Fetch = fetch,
  signal?: AbortSignal,
): Promise<StreamOutcome<T>> {
  return postStream<T>(
    '/api/commands/status/read',
    repo ? { repo } : {},
    'the status read failed',
    (line) => {
      if (line.event === 'partial' && line.result && typeof line.result === 'object') {
        onPartial(line.result, Array.isArray(line.loading) ? line.loading.filter((k) => typeof k === 'string') : [])
      }
    },
    fetcher,
    signal,
  )
}

// postStream posts body as JSON to an NDJSON route with the session and
// reads the stream as applyStream describes; failed is the message of an
// error line without one.
async function postStream<T>(
  path: string,
  body: unknown,
  failed: string,
  onEvent: (line: StreamLine) => void,
  fetcher: Fetch,
  signal?: AbortSignal,
): Promise<StreamOutcome<T>> {
  if (session === null) return failure('session_required', 'no session is open')
  let res: Response
  try {
    res = await fetcher(path, {
      method: 'POST',
      headers: { 'X-Nibfin-Session': session, 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      signal,
    })
  } catch (e) {
    return { ...failure('network_error', `the request to ${path} failed: ${String(e)}`), unknown: true }
  }
  if (!res.ok) {
    let error: Partial<ApiError> | undefined
    try {
      error = ((await res.json()) as { error?: Partial<ApiError> } | null)?.error
    } catch {
      // A body that is not JSON keeps the status as the message.
    }
    return { ...failure(error?.code ?? 'error', error?.message ?? `${path} answered ${res.status}`), status: res.status }
  }
  let last: StreamOutcome<T> | null = null
  const take = (line: string) => {
    if (!line.trim()) return
    let ev: StreamLine
    try {
      ev = JSON.parse(line) as StreamLine
    } catch {
      // A line cut off by a closed stream is not a final line.
      return
    }
    if (ev === null || typeof ev !== 'object') return
    if (ev.event === 'result') last = { ok: true, value: ev.result as T }
    else if (ev.event === 'error') {
      const f: StreamOutcome<T> = failure(ev.error?.code ?? 'error', ev.error?.message ?? failed)
      if (ev.result !== undefined && ev.result !== null) f.result = ev.result as T
      last = f
    }
    onEvent(ev)
  }
  try {
    if (!res.body) {
      for (const line of (await res.text()).split('\n')) take(line)
    } else {
      const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
      let buffered = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffered += value
        const lines = buffered.split('\n')
        buffered = lines.pop() ?? ''
        for (const line of lines) take(line)
      }
      take(buffered)
    }
  } catch (e) {
    return { ...failure('network_error', `the stream of ${path} broke: ${String(e)}`), unknown: true }
  }
  return last ?? { ...failure('stream_incomplete', `the stream of ${path} ended without a result`), unknown: true }
}

// applyPlan applies the stored plan id without confirmation and reads its
// NDJSON stream to the end. The last result line is the result; an error
// line or a non-2xx answer is the error. Other lines are skipped.
export async function applyPlan<T>(id: string, fetcher: Fetch = fetch): Promise<ApplyOutcome<T>> {
  const r = await applyStream<T>(id, false, () => {}, fetcher)
  if (r.ok) return r
  // The pause and resume path keeps its outcomes: an error carries no result.
  const { result: _dropped, ...rest } = r
  return rest
}

// CodeOutcome is the answer of the code route: 204 is ok; otherwise the
// error body's error with its status, or network_error.
export type CodeOutcome = { ok: true } | { ok: false; error: ApiError; status?: number }

// sendCode hands a pasted authorization code to the login waiting in the
// apply of plan id. The code is not kept.
export async function sendCode(id: string, code: string, fetcher: Fetch = fetch): Promise<CodeOutcome> {
  if (session === null) return failure('session_required', 'no session is open')
  const path = `/api/plans/${encodeURIComponent(id)}/code`
  let res: Response
  try {
    res = await fetcher(path, {
      method: 'POST',
      headers: { 'X-Nibfin-Session': session, 'Content-Type': 'application/json' },
      body: JSON.stringify({ code }),
    })
  } catch (e) {
    return failure('network_error', `the request to ${path} failed: ${String(e)}`)
  }
  if (res.status === 204) return { ok: true }
  let error: Partial<ApiError> | undefined
  try {
    error = ((await res.json()) as { error?: Partial<ApiError> } | null)?.error
  } catch {
    // A body that is not JSON keeps the status as the message.
  }
  return { ...failure(error?.code ?? 'error', error?.message ?? `${path} answered ${res.status}`), status: res.status }
}

// deletePlan drops a stored plan that will not be applied.
export async function deletePlan(id: string, fetcher: Fetch = fetch): Promise<void> {
  if (session === null) return
  try {
    await fetcher(`/api/plans/${encodeURIComponent(id)}`, { method: 'DELETE', headers: { 'X-Nibfin-Session': session } })
  } catch {
    // The server drops it when it stops.
  }
}

// resetSession forgets the session; for tests only.
export function resetSession(): void {
  session = null
  exchange = null
}

async function post<T>(fetcher: Fetch, path: string, headers: Record<string, string>, body?: string): Promise<Outcome<T>> {
  return (await send<T>(fetcher, path, { method: 'POST', headers, body })).outcome
}

// send makes one JSON request. It returns the outcome and, for an answer that
// was read, its status and parsed body.
async function send<T>(
  fetcher: Fetch,
  path: string,
  init: RequestInit,
): Promise<{ outcome: Outcome<T>; status?: number; parsed?: unknown }> {
  let res: Response
  let parsed: unknown
  try {
    res = await fetcher(path, init)
    parsed = await res.json()
  } catch (e) {
    return { outcome: failure('network_error', `the request to ${path} failed: ${String(e)}`) }
  }
  const error = (parsed as { error?: Partial<ApiError> } | null)?.error
  if (!res.ok || error) {
    return {
      outcome: failure(error?.code ?? 'error', error?.message ?? `${path} answered ${res.status}`),
      status: res.status,
      parsed,
    }
  }
  return { outcome: { ok: true, value: parsed as T }, status: res.status, parsed }
}

function failure(code: string, message: string): { ok: false; error: ApiError } {
  return { ok: false, error: { code, message } }
}

// currentSession is the open session, or null before the exchange.
export function currentSession(): string | null {
  return session
}
