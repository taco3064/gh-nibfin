import { useState } from 'react'
import { Monitor, Moon, Sun, type LucideIcon } from 'lucide-react'

import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { readPreference, storePreference, type ThemePreference } from '@/lib/theme'

const themeOptions: { value: ThemePreference; label: string; icon: LucideIcon }[] = [
  { value: 'dark', label: 'Dark', icon: Moon },
  { value: 'light', label: 'Light', icon: Sun },
  { value: 'system', label: 'System', icon: Monitor },
]

export function ThemeControl() {
  const [pref, setPref] = useState<ThemePreference>(readPreference)
  return (
    <ToggleGroup
      type="single"
      variant="outline"
      size="sm"
      aria-label="Theme"
      value={pref}
      onValueChange={(v) => {
        // Radix sends '' when the selected item is pressed again; keep the choice.
        if (!v) return
        const next = v as ThemePreference
        setPref(next)
        storePreference(next)
      }}
    >
      {themeOptions.map(({ value, label, icon: Icon }) => (
        <ToggleGroupItem key={value} value={value} className="border-border">
          <Icon aria-hidden="true" />
          {label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}
