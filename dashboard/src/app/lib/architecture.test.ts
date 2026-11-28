import { readFileSync } from 'node:fs'

import { describe, expect, it } from 'vitest'

import { layoutDiagram, type Direction } from '@/app/components/diagram'

import { architecture } from './architecture'

const page = readFileSync(new URL('../content/docs/how-it-works.mdx', import.meta.url), 'utf8')

const node = (id: string) => architecture.nodes.find((n) => n.id === id)
const edge = (from: string, to: string) => architecture.edges.find((e) => e.from === from && e.to === to)

type Box = { x: number; y: number; width: number; height: number }
const inside = (b: Box, x: number, y: number) => x > b.x + 2 && x < b.x + b.width - 2 && y > b.y + 2 && y < b.y + b.height - 2

// crossings lists the edges that run through a box other than their own
// ends, or through another edge's label.
function crossings(direction: Direction): string[] {
  const p = layoutDiagram(architecture, direction)
  const found = new Set<string>()
  for (const e of p.edges) {
    const others: [string, Box][] = [
      ...p.nodes.filter((n) => n.id !== e.from && n.id !== e.to).map((n): [string, Box] => [n.id, n]),
      ...p.edges.filter((o) => o !== e && o.label).map((o): [string, Box] => [`label ${o.from} -> ${o.to}`, o.label!]),
    ]
    for (let i = 0; i + 1 < e.points.length; i++) {
      const a = e.points[i]
      const b = e.points[i + 1]
      for (let t = 0.02; t < 1; t += 0.02) {
        const x = a.x + (b.x - a.x) * t
        const y = a.y + (b.y - a.y) * t
        for (const [name, box] of others) if (inside(box, x, y)) found.add(`${e.from} -> ${e.to} crosses ${name}`)
      }
    }
  }
  return [...found]
}

describe('architecture', () => {
  it('lays out in both directions without an edge crossing a box or label', () => {
    expect(crossings('LR')).toEqual([])
    expect(crossings('TB')).toEqual([])
  })

  it('has the three groups and puts each part in its group', () => {
    expect(architecture.groups?.map((g) => g.id)).toEqual(['person', 'host', 'github'])
    const groups: Record<string, string[]> = {
      person: ['cli', 'dashboard'],
      host: ['machine', 'runners', 'job', 'login'],
      github: ['peer', 'exec', 'hosted', 'image'],
    }
    for (const [group, ids] of Object.entries(groups)) {
      for (const id of ids) expect(node(id)?.group, id).toBe(group)
    }
    expect(architecture.nodes).toHaveLength(10)
  })

  it('labels every edge', () => {
    for (const e of architecture.edges) expect(e.label, `${e.from} -> ${e.to}`).toBeTruthy()
  })

  it('draws the wake, route, dispatch, stage Job and image paths', () => {
    expect(edge('peer', 'exec')?.label).toMatch(/wakes the Issue/)
    expect(edge('exec', 'hosted')?.label).toMatch(/route \(public\)/)
    expect(edge('hosted', 'exec')?.label).toMatch(/dispatches/)
    expect(edge('exec', 'runners')?.label).toMatch(/stage Jobs/)
    expect(edge('exec', 'runners')?.label).toMatch(/route \(private\)/)
    expect(edge('runners', 'job')).toBeDefined()
    expect(edge('machine', 'runners')).toBeDefined()
    expect(edge('image', 'job')?.label).toMatch(/pull by digest/)
    expect(edge('login', 'job')?.label).toMatch(/mounted/)
    expect(node('dashboard')?.detail).toMatch(/gh nibfin dashboard/)
    expect(node('dashboard')?.detail).toMatch(/127\.0\.0\.1 only/)
  })

  it('shows the Secret in both repositories and the token path into the Job', () => {
    for (const repo of ['peer', 'exec']) {
      expect(node(repo)?.badges).toContain('NIBFIN_GITHUB_TOKEN')
      expect(edge('cli', repo)?.label).toMatch(/^init: Secret/)
    }
    const intoJob = architecture.edges.filter((e) => e.to === 'job' && /Secret|token/i.test(e.label ?? ''))
    expect(intoJob).toHaveLength(1)
    expect(intoJob[0].label).toMatch(/for Nibfin, never the agent/)
  })

  it('draws pushes and pull requests to both repositories', () => {
    for (const repo of ['peer', 'exec']) expect(edge('job', repo)?.label).toMatch(/pushes and pull requests/)
  })

  it('uses only badge terms the page states', () => {
    for (const n of architecture.nodes) {
      for (const badge of n.badges ?? []) expect(page, badge).toContain(badge)
    }
  })
})
