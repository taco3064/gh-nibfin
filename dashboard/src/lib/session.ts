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
