import { digits } from '@/app/lib/summary'

const wheel = ['0', '1', '2', '3', '4', '5', '6', '7', '8', '9']

// RollingNumber shows a count whose digits roll like an odometer when it
// changes; under reduced motion they change at once. Screen readers get
// the value as text.
export function RollingNumber({ value, className }: { value: number | null; className?: string }) {
  const cols = digits(value)
  if (cols.length === 0) {
    return (
      <span className={className} data-slot="rolling-number" data-value="">
        <span aria-hidden="true">—</span>
        <span className="sr-only">Not read yet</span>
      </span>
    )
  }
  return (
    <span className={className} data-slot="rolling-number" data-value={value ?? ''}>
      <span className="sr-only">{value?.toLocaleString('en-US')}</span>
      <span aria-hidden="true" className="inline-flex tabular-nums">
        {cols.map((c, i) => {
          // Columns are keyed from the right, so a new leading digit leaves
          // the others in place.
          const key = cols.length - i
          if (c === ',') return <span key={`s${key}`}>,</span>
          return (
            <span key={key} className="relative inline-block h-[1.15em] overflow-hidden">
              <span
                className="flex flex-col ease-out motion-safe:transition-transform motion-safe:duration-700"
                style={{ transform: `translateY(-${Number(c) * 10}%)` }}
              >
                {wheel.map((d) => (
                  <span key={d} className="block h-[1.15em] leading-[1.15]">
                    {d}
                  </span>
                ))}
              </span>
            </span>
          )
        })}
      </span>
    </span>
  )
}
