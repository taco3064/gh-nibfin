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

// ApplyOutcome is the end of an apply. unknown is set when the request failed
// before the final line arrived: the write may or may not have happened.
export type ApplyOutcome<T> = { ok: true; value: T } | { ok: false; error: ApiError; unknown?: boolean; status?: number }

// applyPlan applies the stored plan id without confirmation and reads its
// NDJSON stream to the end. The last result line is the result; an error
// line or a non-2xx answer is the error. login_step lines are skipped.
export async function applyPlan<T>(id: string, fetcher: Fetch = fetch): Promise<ApplyOutcome<T>> {
  if (session === null) return failure('session_required', 'no session is open')
  const path = `/api/plans/${encodeURIComponent(id)}/apply`
  let res: Response
  try {
    res = await fetcher(path, {
      method: 'POST',
      headers: { 'X-Nibfin-Session': session, 'Content-Type': 'application/json' },
      body: JSON.stringify({ confirm: false }),
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
  let text: string
  try {
    text = await res.text()
  } catch (e) {
    return { ...failure('network_error', `the stream of ${path} broke: ${String(e)}`), unknown: true }
  }
  let last: ApplyOutcome<T> | null = null
  for (const line of text.split('\n')) {
    if (!line.trim()) continue
    let ev: { event?: string; result?: T; error?: Partial<ApiError> }
    try {
      ev = JSON.parse(line)
    } catch {
      // A line cut off by a closed stream is not a final line.
      continue
    }
    if (ev.event === 'result') last = { ok: true, value: ev.result as T }
    else if (ev.event === 'error') last = failure(ev.error?.code ?? 'error', ev.error?.message ?? 'the apply failed')
  }
  return last ?? { ...failure('stream_incomplete', `the stream of ${path} ended without a result`), unknown: true }
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
  let res: Response
  let parsed: unknown
  try {
    res = await fetcher(path, { method: 'POST', headers, body })
    parsed = await res.json()
  } catch (e) {
    return failure('network_error', `the request to ${path} failed: ${String(e)}`)
  }
  const error = (parsed as { error?: Partial<ApiError> } | null)?.error
  if (!res.ok || error) {
    return failure(error?.code ?? 'error', error?.message ?? `${path} answered ${res.status}`)
  }
  return { ok: true, value: parsed as T }
}

function failure(code: string, message: string): { ok: false; error: ApiError } {
  return { ok: false, error: { code, message } }
}
