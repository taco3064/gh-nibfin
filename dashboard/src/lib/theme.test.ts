import { describe, expect, it } from 'vitest'

import { parsePreference, resolveTheme } from './theme'

describe('resolveTheme', () => {
  it.each([
    ['dark', false, 'dark'],
    ['dark', true, 'dark'],
    ['light', false, 'light'],
    ['light', true, 'light'],
    ['system', false, 'dark'],
    ['system', true, 'light'],
  ] as const)('%s with prefersLight=%s is %s', (pref, prefersLight, want) => {
    expect(resolveTheme(pref, prefersLight)).toBe(want)
  })
})

describe('parsePreference', () => {
  it.each([
    ['dark', 'dark'],
    ['light', 'light'],
    ['system', 'system'],
    [null, 'dark'],
    ['', 'dark'],
    ['Light', 'dark'],
    ['auto', 'dark'],
  ] as const)('%j is %s', (stored, want) => {
    expect(parsePreference(stored)).toBe(want)
  })
})
