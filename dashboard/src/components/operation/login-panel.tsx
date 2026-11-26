import { Check, CircleAlert, CircleStop, Copy, ExternalLink, LoaderCircle } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { tones } from '@/components/issue/tones'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { endsAt, providerTitle, running, safeUrl, type LoginStage, type LoginState } from '@/lib/login'
import { sendCode, type ApiError } from '@/lib/session'
import { cn, formatClock } from '@/lib/utils'

type Fetch = typeof fetch

const steps: { label: string; stages: LoginStage[] }[] = [
  { label: 'Waiting for authorization', stages: ['authorizing'] },
  { label: 'Verifying', stages: ['verifying'] },
  { label: 'Done', stages: ['done'] },
]

// LoginSteps is the ordered list of a login's steps with the current one
// marked; a failed or ended login marks none and shows its message.
function LoginSteps({ stage }: { stage: LoginStage }) {
  const at = steps.findIndex((s) => s.stages.includes(stage))
  return (
    <ol aria-label="Login steps" className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs">
      {steps.map((s, i) => {
        const past = at >= 0 && i < at
        const current = i === at
        return (
          <li key={s.label} className="flex items-center gap-1.5">
            <span
              aria-current={current ? 'step' : undefined}
              data-step-state={current ? 'current' : past ? 'past' : 'next'}
              className={cn(
                'inline-flex items-center gap-1 rounded-full border px-2 py-0.5',
                current
                  ? 'border-primary bg-primary font-semibold text-primary-foreground'
                  : past
                    ? 'border-border text-foreground'
                    : 'border-border text-muted-foreground',
              )}
            >
              {past && <Check aria-hidden="true" className="size-3" />}
              {current && stage !== 'done' && <LoaderCircle aria-hidden="true" className="size-3 motion-safe:animate-spin" />}
              <span className="tabular-nums">{i + 1}.</span> {s.label}
              {past && <span className="sr-only"> (done)</span>}
            </span>
            {i < steps.length - 1 && (
              <span aria-hidden="true" className="text-muted-foreground">
                ›
              </span>
            )}
          </li>
        )
      })}
    </ol>
  )
}

function SignInLink({ url }: { url?: string }) {
  if (!url) return null
  const safe = safeUrl(url)
  return (
    <div className="flex flex-col gap-2">
      {safe && (
        <Button asChild className="w-full sm:w-fit">
          <a href={safe} target="_blank" rel="noreferrer noopener" data-action="open-url">
            <ExternalLink aria-hidden="true" />
            Open sign-in page
          </a>
        </Button>
      )}
      <p className="text-xs text-muted-foreground">
        {safe ? 'Or open this URL in a browser:' : 'Open this URL in a browser:'}
      </p>
      <p
        data-slot="login-url"
        className="rounded-md border border-border-subtle bg-[var(--nf-code-bg)] px-2 py-1.5 font-mono text-xs break-all text-[var(--nf-code)] select-all"
      >
        {url}
      </p>
    </div>
  )
}

function DeviceCode({ code }: { code: string }) {
  const [copy, setCopy] = useState<'idle' | 'copied' | 'failed'>('idle')
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopy('copied')
    } catch {
      setCopy('failed')
    }
  }
  return (
    <div className="flex flex-col gap-2">
      <p className="text-sm">Enter this one-time code on the sign-in page:</p>
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <p
          data-slot="device-code"
          className="rounded-md border border-border bg-[var(--nf-code-bg)] px-3 py-2 text-center font-mono text-2xl font-semibold tracking-widest break-all text-[var(--nf-code)] select-all sm:text-left"
        >
          {code}
        </p>
        <Button variant="outline" onClick={() => void onCopy()} data-action="copy-code">
          {copy === 'copied' ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
          {copy === 'copied' ? 'Copied' : 'Copy code'}
        </Button>
      </div>
      <p role="status" data-slot="copy-state" className="text-xs text-muted-foreground">
        {copy === 'failed' ? 'Copy failed: select the code' : ''}
      </p>
    </div>
  )
}

function CodeForm({
  state,
  planId,
  fetcher,
  onSent,
}: {
  state: LoginState
  planId?: string
  fetcher?: Fetch
  onSent: () => void
}) {
  const [code, setCode] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<ApiError>()
  const id = `login-code-${state.provider}`
  const disabled = state.codeSent || sending || !running(state)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const v = code.trim()
    if (!planId || v === '' || disabled) return
    setSending(true)
    setError(undefined)
    const r = await sendCode(planId, v, fetcher)
    setSending(false)
    if (r.ok) {
      setCode('')
      onSent()
    } else setError(r.error)
  }

  return (
    <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-2" data-slot="code-form" noValidate>
      <Label htmlFor={id}>Authorization code</Label>
      <p id={`${id}-help`} className="text-xs text-muted-foreground">
        Paste the code the sign-in page shows after you authorize.
      </p>
      <div className="flex flex-col gap-2 sm:flex-row">
        <Input
          id={id}
          value={code}
          onChange={(e) => setCode(e.target.value)}
          autoComplete="off"
          spellCheck={false}
          disabled={disabled}
          aria-describedby={`${id}-help`}
          aria-invalid={error ? true : undefined}
          className="font-mono"
        />
        <Button type="submit" disabled={disabled || code.trim() === ''}>
          {sending && <LoaderCircle aria-hidden="true" className="motion-safe:animate-spin" />}
          {sending ? 'Submitting…' : 'Submit code'}
        </Button>
      </div>
      {state.codeSent && (
        <p data-slot="code-sent" className="text-xs text-muted-foreground">
          The code was sent once to the provider&apos;s login.
        </p>
      )}
      {error && (
        <p role="alert" data-slot="code-error" className="text-sm break-words text-danger">
          <code className="font-semibold">{error.code}</code>: {error.message}
        </p>
      )}
    </form>
  )
}

// LoginPanel shows one provider's login of the CLI's own flow: the URL,
// then the pasted code field (Claude) or the one-time code to copy (Codex),
// and its steps to the read-back.
export function LoginPanel({
  state,
  planId,
  fetcher,
  onCodeSent,
}: {
  state: LoginState
  planId?: string
  fetcher?: Fetch
  onCodeSent: (provider: string) => void
}) {
  const live = running(state)
  const titleId = `login-title-${state.provider}`
  return (
    <section
      aria-labelledby={titleId}
      data-login={state.provider}
      data-stage={state.stage}
      className="flex flex-col gap-3 rounded-lg border border-border bg-background p-3 sm:p-4"
    >
      <div className="flex flex-col gap-2">
        <h3 id={titleId} className="text-sm font-semibold">
          Sign in to {providerTitle(state.provider)}
        </h3>
        <LoginSteps stage={state.stage} />
      </div>
      {state.stage === 'authorizing' && <SignInLink url={state.url} />}
      {state.stage === 'authorizing' && state.deviceCode && !state.codeRequested && <DeviceCode code={state.deviceCode} />}
      {live && state.codeRequested && (
        <CodeForm state={state} planId={planId} fetcher={fetcher} onSent={() => onCodeSent(state.provider)} />
      )}
      {state.stage === 'authorizing' && (
        <p data-slot="login-timeout" className="text-xs text-muted-foreground">
          Ends at <time dateTime={new Date(endsAt(state)).toISOString()}>{formatClock(endsAt(state))}</time> if not authorized.
        </p>
      )}
      {state.stage === 'done' && (
        <p data-slot="login-message" className={cn('flex items-start gap-2 rounded-md px-3 py-2 text-sm', tones.success)}>
          <Check aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
          Logged in: confirmed by the read-back.
        </p>
      )}
      {(state.stage === 'failed' || state.stage === 'ended') && (
        <p
          data-slot="login-message"
          className={cn('flex items-start gap-2 rounded-md px-3 py-2 text-sm break-words', state.stage === 'failed' ? tones.danger : tones.warning)}
        >
          {state.stage === 'failed' ? (
            <CircleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
          ) : (
            <CircleStop aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
          )}
          <span className="min-w-0">
            <span className="font-medium">{state.stage === 'failed' ? 'Failed' : 'Ended'}:</span> {state.message}
          </span>
        </p>
      )}
    </section>
  )
}
