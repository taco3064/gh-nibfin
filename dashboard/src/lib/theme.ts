// Theme preference: dark by default, explicit choices persisted, and System
// following the operating system live, as the Nibfin theme requires.

export type ThemePreference = 'dark' | 'light' | 'system'
export type Theme = 'dark' | 'light'

export const themeStorageKey = 'nibfin-dashboard-theme'

const lightQuery = '(prefers-color-scheme: light)'

// parsePreference maps a stored value to a preference; anything else is dark.
export function parsePreference(value: string | null): ThemePreference {
  return value === 'light' || value === 'system' ? value : 'dark'
}

export function resolveTheme(pref: ThemePreference, prefersLight: boolean): Theme {
  if (pref === 'system') return prefersLight ? 'light' : 'dark'
  return pref
}

export function readPreference(): ThemePreference {
  try {
    return parsePreference(localStorage.getItem(themeStorageKey))
  } catch {
    return 'dark'
  }
}

let media: MediaQueryList | null = null
const followSystem = () => applyTheme()

// applyTheme sets data-theme from the stored preference and, under System,
// keeps following prefers-color-scheme until another choice is stored.
export function applyTheme(pref: ThemePreference = readPreference()): void {
  media ??= window.matchMedia(lightQuery)
  media.removeEventListener('change', followSystem)
  if (pref === 'system') media.addEventListener('change', followSystem)
  document.documentElement.dataset.theme = resolveTheme(pref, media.matches)
}

export function storePreference(pref: ThemePreference): void {
  try {
    localStorage.setItem(themeStorageKey, pref)
  } catch {
    // Storage can be unavailable; the choice still applies to this page.
  }
  applyTheme(pref)
}
