// The connections and the alarm loop of the one Durable Object. It knows
// the runtime only through Store, so tests pass fakes.

import { publicData } from './build'
import type { GitHub } from './github'
import type { ApiError, LiveEvent } from './messages'
import { check, minInterval, type CheckResult } from './poller'
import { currentErrors, initialState, type State } from './state'

export interface Socket {
  send(message: string): void
  close(code?: number, reason?: string): void
}

export interface Store {
  load(): Promise<State | undefined>
  save(state: State): Promise<void>
  sockets(): Socket[]
  getAlarm(): Promise<number | null>
  setAlarm(ms: number): Promise<void>
}

export interface HubDeps {
  store: Store
  github: () => GitHub // a new client, with a new request budget, per check
  now: () => number
}

const iso = (ms: number) => new Date(ms).toISOString()

function send(socket: Socket, events: LiveEvent[]): void {
  try {
    for (const e of events) socket.send(JSON.stringify(e))
  } catch {
    try {
      socket.close(1011, 'send failed')
    } catch {
      // The socket is already gone.
    }
  }
}

function broadcast(store: Store, events: LiveEvent[]): void {
  if (events.length === 0) return
  for (const socket of store.sockets()) send(socket, events)
}

function snapshot(state: State): LiveEvent[] {
  return state.data && state.at !== null ? [{ event: 'snapshot', at: iso(state.at), result: publicData(state.data) }] : []
}

function errorEvents(errors: ApiError[]): LiveEvent[] {
  return errors.map((error) => ({ event: 'error', source: 'github', error }))
}

export function createHub({ store, github, now }: HubDeps) {
  return {
    // connect greets a newly accepted socket with the stored snapshot, then
    // checked with the last good time or the current errors, and starts
    // polling when it was stopped.
    async connect(socket: Socket): Promise<void> {
      const state = await store.load()
      if (state) {
        const errors = currentErrors(state)
        const status: LiveEvent[] = errors.length > 0 ? errorEvents(errors) : state.okAt !== null ? [{ event: 'checked', at: iso(state.okAt) }] : []
        send(socket, [...snapshot(state), ...status])
      }
      if ((await store.getAlarm()) === null) await store.setAlarm(now())
    },

    // alarm runs one check while any page is connected and pushes what
    // changed. The next alarm is set before GitHub is called, so neither a
    // failure nor an exception ends the polling.
    async alarm(): Promise<void> {
      let state = (await store.load()) ?? initialState()
      if (state.seq > state.sentSeq) {
        broadcast(store, snapshot(state))
        state.sentSeq = state.seq
        await store.save(state)
      }
      if (store.sockets().length === 0) return
      const start = now()
      await store.setAlarm(start + minInterval)

      let result: CheckResult
      try {
        result = await check(state, github(), start)
      } catch (e) {
        const error: ApiError = { code: 'github_api_failed', message: `the check failed: ${e instanceof Error ? e.message : String(e)}` }
        result = { state, changed: false, errors: [error] }
      }
      const prevOk = state.okAt
      state = result.state
      await store.save(state)

      const events: LiveEvent[] = result.changed ? snapshot(state) : []
      if (result.errors.length > 0) events.push(...errorEvents(result.errors))
      else if (state.okAt !== null && (prevOk === null || state.okAt > prevOk)) events.push({ event: 'checked', at: iso(state.okAt) })
      broadcast(store, events)
      if (state.sentSeq !== state.seq) {
        state.sentSeq = state.seq
        await store.save(state)
      }
    },
  }
}
