// Token tones of the Issue element's badges and notes. Every tone goes with
// a text label; color never carries the meaning alone.
export const tones = {
  neutral: 'border-border bg-muted text-foreground',
  info: 'border-transparent bg-info-bg text-info',
  success: 'border-transparent bg-success-bg text-success',
  warning: 'border-transparent bg-warning-bg text-warning',
  danger: 'border-transparent bg-danger-bg text-danger',
} as const

export type Tone = keyof typeof tones
