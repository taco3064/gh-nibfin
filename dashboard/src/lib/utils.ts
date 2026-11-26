import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// The Dashboard shows every time in one English format in the viewer's time
// zone, whatever the browser locale: "26 Nov 2026, 02:14" and "09:18:43".
const dateTime = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  hourCycle: 'h23',
})
const clock = new Intl.DateTimeFormat('en-GB', {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
})

// formatTime shows an ISO time as a date and time, or the text as it is when
// it is not a time.
export function formatTime(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  return dateTime.format(t)
}

// formatClock shows the time of day of a time.
export function formatClock(t: number | Date): string {
  return clock.format(t)
}
