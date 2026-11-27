// A Sentry `issue` webhook's data.issue as Sentry's documentation example
// shows it, trimmed to what the relay reads plus a few fields it drops.
export function webhookIssue(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: '1234567890',
    shortId: 'NIBFIN-WEBSITE-1A',
    title: "TypeError: Cannot read properties of undefined (reading 'map')",
    culprit: 'renderDiagram(src/components/diagram)',
    permalink: 'https://na-43k.sentry.io/issues/1234567890/',
    web_url: 'https://na-43k.sentry.io/issues/1234567890/?web',
    level: 'error',
    status: 'unresolved',
    project: { id: '1', name: 'nibfin-website', slug: 'nibfin-website' },
    count: '3',
    userCount: 2,
    firstSeen: '2026-11-27T01:02:03.456000Z',
    lastSeen: '2026-11-27T04:05:06.789000Z',
    metadata: { value: 'secret-looking text that is not forwarded' },
    ...overrides,
  }
}
