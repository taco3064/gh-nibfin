import { freeFraction, gib } from '@/lib/runner'
import type { MemoryReport } from '@/lib/status'

// The half ring: a 180° arc from left to right around (60, 60).
const arc = 'M 10 60 A 50 50 0 0 1 110 60'

// MemoryGauge is the host's free memory as a half-ring meter with the free
// and total GiB. The ring is the meter's picture; its text says the same.
export function MemoryGauge({ memory }: { memory: MemoryReport }) {
  const f = freeFraction(memory)
  if (f === null) return null
  const pct = Math.round(f * 100)
  const text = `${gib(memory.availableKB)} free of ${gib(memory.totalKB)}`
  return (
    <div
      role="meter"
      aria-label="Free memory"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={pct}
      aria-valuetext={`${pct}% free, ${text}`}
      data-slot="memory-gauge"
      className="flex items-center gap-4"
    >
      <div className="relative w-28 shrink-0">
        <svg viewBox="0 0 120 66" aria-hidden="true" className="block w-full overflow-visible">
          <path d={arc} pathLength={100} fill="none" strokeWidth={10} strokeLinecap="round" className="stroke-muted" />
          {pct > 0 && (
            <path d={arc} pathLength={100} fill="none" strokeWidth={10} strokeLinecap="round" strokeDasharray={`${pct} 100`} className="stroke-primary" />
          )}
        </svg>
        <span aria-hidden="true" className="absolute inset-x-0 bottom-0 text-center font-heading text-lg leading-none font-semibold tabular-nums">
          {pct}%
        </span>
      </div>
      <p data-slot="memory" className="text-sm">
        <span className="font-semibold tabular-nums">{gib(memory.availableKB)}</span> <span className="text-muted-foreground">free of</span>{' '}
        <span className="tabular-nums">{gib(memory.totalKB)}</span>
      </p>
    </div>
  )
}
