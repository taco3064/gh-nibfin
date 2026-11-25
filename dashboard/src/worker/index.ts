// The public live data Worker: GET /live with a WebSocket upgrade connects
// to the one Durable Object, which polls GitHub with the read-only token
// and pushes the same data to every page. The token stays in env.

import { DurableObject } from 'cloudflare:workers'

import { createGitHub } from './github'
import { createHub, type Store } from './hub'
import type { State } from './state'

export interface Env {
  LIVE: DurableObjectNamespace<Live>
  GITHUB_TOKEN?: string
}

export class Live extends DurableObject<Env> {
  private hub() {
    const ctx = this.ctx
    const store: Store = {
      load: () => ctx.storage.get<State>('state'),
      save: (state) => ctx.storage.put('state', state),
      sockets: () => ctx.getWebSockets(),
      getAlarm: () => ctx.storage.getAlarm(),
      setAlarm: (ms) => ctx.storage.setAlarm(ms),
    }
    return createHub({ store, github: () => createGitHub({ token: this.env.GITHUB_TOKEN, fetch: (input, init) => fetch(input, init) }), now: () => Date.now() })
  }

  async fetch(): Promise<Response> {
    const { 0: client, 1: server } = new WebSocketPair()
    this.ctx.acceptWebSocket(server)
    await this.hub().connect(server)
    return new Response(null, { status: 101, webSocket: client })
  }

  async alarm(): Promise<void> {
    await this.hub().alarm()
  }

  // Pages send nothing the Worker reads.
  webSocketMessage(): void {}

  // The runtime completes the close handshake itself.
  webSocketClose(): void {}
}

export default {
  async fetch(request, env): Promise<Response> {
    const url = new URL(request.url)
    if (url.pathname !== '/live') return new Response('not found', { status: 404 })
    if (request.method !== 'GET' || request.headers.get('Upgrade')?.toLowerCase() !== 'websocket') {
      return new Response('this endpoint needs a WebSocket upgrade', { status: 426, headers: { Upgrade: 'websocket' } })
    }
    return env.LIVE.getByName('live').fetch(request)
  },
} satisfies ExportedHandler<Env>
