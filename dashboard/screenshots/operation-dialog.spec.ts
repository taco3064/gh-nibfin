import { expect, test, type Locator, type Page } from '@playwright/test'

import { commands } from './command-fixtures'
import { expectClean, expectNonce, fulfillPlanStream, prepare, shot, storageKey, widths } from './support'

// Screenshots and checks of the operation dialog (#351) in its harness
// (operation.html), served by the real Go server with its CSP. The schema,
// plan, apply and delete routes are mocked with fictional bodies.

const themes = ['dark', 'light'] as const

const step = (id: string, action: string, target: string, extra: Record<string, unknown> = {}) => ({
  id,
  action,
  target,
  noChange: false,
  afterStart: false,
  ...extra,
})

const plans: Record<string, unknown> = {
  'runner-add': {
    operation: 'runner add',
    steps: [
      step('machine', 'start machine', 'nibfin-machine', { current: 'stopped', next: 'running' }),
      step('image', 'pull image', 'ghcr.io/acme/nibfin-runner:1.4.0', { current: '1.4.0', next: '1.4.0', noChange: true }),
      step('login', 'log in', 'claude', { detail: 'You may be asked to open a URL and paste a code.', next: 'logged in' }),
      step('slots', 'start Runners', '2 new slots', { next: 'running', afterStart: true }),
    ],
    notes: ['Runners pick up Issues from every repository the account can reach.'],
    confirm: true,
    readOnly: false,
  },
  'runner-remove': {
    operation: 'runner remove',
    steps: [
      step('slot:claude-1', 'remove Runner', 'claude-1', { current: 'idle', next: 'absent' }),
      step('slot:claude-2', 'remove Runner', 'claude-2', { current: 'idle', next: 'absent' }),
      step('login', 'keep login', 'claude', { current: 'logged in', next: 'logged in', noChange: true }),
    ],
    confirm: true,
    readOnly: false,
  },
  init: {
    operation: 'init',
    repo: 'acme/app',
    steps: [
      step('label:scope', 'create label', 'acme/app scope', { current: 'absent', next: 'present' }),
      step('secret:token', 'set Secret', 'acme/app NIBFIN_TOKEN', { current: 'absent', next: 'set' }),
    ],
    notes: ['Allow GitHub Actions to create pull requests in the repository settings.'],
    confirm: false,
    readOnly: false,
  },
}

const ids: Record<string, string> = { 'runner-add': 'PLANADD', 'runner-remove': 'PLANREMOVE', init: 'PLANINIT' }

const progress = (id: string, action: string, target: string, phase: string, extra: Record<string, unknown> = {}) =>
  JSON.stringify({ event: 'progress', progress: { id, action, target, phase, ...extra } })

const check = (id: string, action: string, target: string, confirmed: boolean, state: string, reason?: string) => ({
  id,
  action,
  target,
  confirmed,
  state,
  ...(reason && { reason }),
})

const applyLines: Record<string, string[]> = {
  PLANADD: [
    progress('machine', 'start machine', 'nibfin-machine', 'started'),
    progress('machine', 'start machine', 'nibfin-machine', 'done'),
    progress('login', 'log in', 'claude', 'started'),
    progress('login', 'log in', 'claude', 'done'),
    progress('slot:claude-3', 'start Runner', 'claude-3', 'started'),
    progress('slot:claude-3', 'start Runner', 'claude-3', 'failed', {
      code: 'runner_busy',
      message: 'slot claude-3 is still registering; run gh nibfin runner add again to finish it',
    }),
    progress('slot:claude-4', 'start Runner', 'claude-4', 'started'),
    progress('slot:claude-4', 'start Runner', 'claude-4', 'done'),
    JSON.stringify({
      event: 'error',
      error: { code: 'runner_busy', message: 'slot claude-3 is still registering; run gh nibfin runner add again to finish it' },
      result: {
        items: [
          check('machine', 'start machine', 'nibfin-machine', true, 'running'),
          check('image', 'pull image', 'ghcr.io/acme/nibfin-runner:1.4.0', true, '1.4.0'),
          check('login', 'log in', 'claude', true, 'logged in'),
          check('slot:claude-3', 'start Runner', 'claude-3', false, 'registering', 'the Runner is not online on GitHub yet'),
          check('slot:claude-4', 'start Runner', 'claude-4', true, 'running'),
        ],
      },
    }),
  ],
  PLANREMOVE: [
    progress('slot:claude-1', 'remove Runner', 'claude-1', 'started'),
    progress('slot:claude-1', 'remove Runner', 'claude-1', 'done'),
    progress('slot:claude-2', 'remove Runner', 'claude-2', 'started'),
    progress('slot:claude-2', 'remove Runner', 'claude-2', 'done'),
    JSON.stringify({
      event: 'result',
      result: {
        removed: 2,
        items: [
          check('slot:claude-1', 'remove Runner', 'claude-1', true, 'absent'),
          check('slot:claude-2', 'remove Runner', 'claude-2', true, 'absent'),
          check('login', 'keep login', 'claude', true, 'logged in'),
        ],
      },
    }),
  ],
  PLANINIT: [progress('label:scope', 'create label', 'acme/app scope', 'started'), progress('label:scope', 'create label', 'acme/app scope', 'done'), '{"event":"res'],
}

// The login streams (#354): a provider login without a terminal, as the
// server emits it, in fictional values. loginLines(provider, end) is the
// progress start, the URL, the code step, then end.
const loginStep = (provider: string, kind: string, extra: Record<string, unknown> = {}) =>
  JSON.stringify({ event: 'login_step', step: { provider, kind, ...extra } })

const claudeURL =
  'https://claude.ai/oauth/authorize?code=true&client_id=00000000-fict-ional-0000-000000000000&response_type=code&redirect_uri=https%3A%2F%2Fconsole.example%2Foauth%2Fcode%2Fcallback&state=FICTIONALSTATEVALUE'
const codexURL = 'https://auth.openai.com/codex/device'
const deviceCode = 'WXYZ-7K3Q9'

const loginPlan = (provider: string, extra: Record<string, unknown> = {}) => ({
  operation: 'login',
  steps: [
    step('host', 'start', 'podman machine nibfin-dev', { current: 'running', next: 'running', noChange: true }),
    step(`volume:nibfin-home-${provider}`, 'create volume', `nibfin-home-${provider}`, { current: 'present', next: 'present', noChange: true }),
    step(`login:${provider}`, 'log in', provider, {
      detail: 'podman machine nibfin-dev; logs in again when already logged in',
      current: 'not logged in',
      next: 'logged in',
    }),
  ],
  confirm: false,
  readOnly: false,
  ...extra,
})

const loginItems = (provider: string, confirmed: boolean) => [
  check('host', 'start', 'podman machine nibfin-dev', true, 'running'),
  check(`volume:nibfin-home-${provider}`, 'create volume', `nibfin-home-${provider}`, true, 'present'),
  check(`login:${provider}`, 'log in', provider, confirmed, confirmed ? 'logged in' : 'not logged in', confirmed ? undefined : `${provider} is not logged in`),
]

const loginLines: Record<string, string[]> = {
  // Held at 3: waiting for the pasted code.
  PLANLOGINCLAUDE: [
    progress('login:claude', 'log in', 'claude', 'started'),
    loginStep('claude', 'open_url', { url: claudeURL, message: 'open this URL in a browser and sign in' }),
    loginStep('claude', 'code_requested', { message: 'paste the code shown in your browser' }),
    loginStep('claude', 'succeeded', { message: 'claude login finished' }),
    progress('login:claude', 'log in', 'claude', 'done'),
    JSON.stringify({ event: 'result', result: { host: 'podman machine nibfin-dev', providers: { claude: { loggedIn: true } }, items: loginItems('claude', true) } }),
  ],
  // Held at 3: the one-time code is shown; 4 is the CLI's success.
  PLANLOGINCODEX: [
    progress('login:codex', 'log in', 'codex', 'started'),
    loginStep('codex', 'open_url', { url: codexURL, message: 'open this URL in a browser and sign in' }),
    loginStep('codex', 'device_code', { code: deviceCode, message: 'enter this one-time code at the URL' }),
    loginStep('codex', 'succeeded', { message: 'codex login finished' }),
    progress('login:codex', 'log in', 'codex', 'done'),
    JSON.stringify({ event: 'result', result: { host: 'podman machine nibfin-dev', providers: { codex: { loggedIn: true } }, items: loginItems('codex', true) } }),
  ],
  // runner add logs codex in, and the CLI fails.
  PLANADDCODEX: [
    progress('login:codex', 'log in', 'codex', 'started'),
    loginStep('codex', 'open_url', { url: codexURL, message: 'open this URL in a browser and sign in' }),
    loginStep('codex', 'device_code', { code: deviceCode, message: 'enter this one-time code at the URL' }),
    loginStep('codex', 'failed', { message: 'codex login exited with status 1' }),
    progress('login:codex', 'log in', 'codex', 'failed', { code: 'login_failed', message: 'codex login exited with status 1' }),
    JSON.stringify({
      event: 'error',
      error: { code: 'login_failed', message: 'codex login exited with status 1' },
      result: { login: { codex: { loggedIn: false } }, items: [check('login:codex', 'log in', 'codex', false, 'not logged in', 'codex is not logged in')] },
    }),
  ],
  // A busy Runner: its item stays in progress until its Job ends (held at 1).
  PLANREMOVEBUSY: [
    progress('slot:codex-1', 'remove Runner', 'codex-1', 'started'),
    progress('slot:codex-1', 'remove Runner', 'codex-1', 'done'),
    JSON.stringify({ event: 'result', result: { removed: 1, items: [check('slot:codex-1', 'remove Runner', 'codex-1', true, 'absent')] } }),
  ],
}

const providerPlans: Record<string, { id: string; plan: unknown }> = {
  'login:claude': { id: 'PLANLOGINCLAUDE', plan: loginPlan('claude') },
  'login:codex': { id: 'PLANLOGINCODEX', plan: loginPlan('codex') },
  'runner-add:codex': {
    id: 'PLANADDCODEX',
    plan: {
      operation: 'runner add',
      steps: [step('login:codex', 'log in', 'codex', { current: 'not logged in', next: 'logged in' }), step('slots', 'start Runners', '1 new slot', { next: 'running' })],
      confirm: true,
      readOnly: false,
    },
  },
  'runner-remove:codex': {
    id: 'PLANREMOVEBUSY',
    plan: {
      operation: 'runner remove',
      steps: [step('slot:codex-1', 'remove Runner', 'codex-1', { detail: 'nibfin-codex-1, busy: waits for its Job to end', current: 'busy', next: 'absent' })],
      confirm: true,
      readOnly: false,
    },
  },
}

interface Api {
  plans: { api: string; body: unknown }[]
  applies: { id: string; body: unknown }[]
  deletes: string[]
  // refuse, when set, answers the next apply with this status.
  refuse?: number
  codes: { id: string; body: unknown }[]
  // codeStatus answers the code route; 204 by default.
  codeStatus?: number
}

async function mockOps(page: Page): Promise<Api> {
  const api: Api = { plans: [], applies: [], deletes: [], codes: [] }
  await page.route('**/api/commands', (route) => {
    expect(route.request().method()).toBe('GET')
    expect(route.request().headers()['x-nibfin-session']).toBe('SCREENSHOTSESSION')
    return route.fulfill({ json: { commands } })
  })
  await page.route('**/api/commands/*/plan/stream', (route) => {
    const name = new URL(route.request().url()).pathname.split('/')[3]
    expect(route.request().headers()['x-nibfin-session']).toBe('SCREENSHOTSESSION')
    const body = route.request().postDataJSON() as Record<string, unknown>
    api.plans.push({ api: name, body })
    const pp = typeof body.provider === 'string' ? providerPlans[`${name}:${body.provider}`] : undefined
    if (pp) return fulfillPlanStream(route, pp as { id: string; plan: { steps: [] } })
    if (name === 'login') {
      return fulfillPlanStream(route, {
        plan: { operation: 'login', steps: [step('login:claude', 'log in', 'claude', { current: 'logged in', next: 'logged in', noChange: true })], confirm: false, readOnly: true },
        result: { items: [check('login:claude', 'log in', 'claude', true, 'logged in')], notes: ['Every provider is already logged in.'] },
      })
    }
    if (name === 'upgrade') {
      if (body.repo === 'acme/broken') {
        return fulfillPlanStream(route, {
          error: { code: 'github_error', message: 'reading the workflows of acme/broken: HTTP 502' },
          plan: { operation: 'upgrade', repo: 'acme/broken', steps: [step('workflow', 'keep workflow', '.github/workflows/nibfin.yml', { noChange: true })], confirm: false, readOnly: true },
          result: { items: [check('workflow', 'keep workflow', '.github/workflows/nibfin.yml', false, '', 'the read failed: HTTP 502')] },
        })
      }
      // A repository that cannot be resolved fails before the stream.
      return route.fulfill({
        status: 422,
        json: { error: { code: 'not_a_repository', message: 'the directory is not a Git repository; run it in a repository or set --repo' } },
      })
    }
    return fulfillPlanStream(route, { id: ids[name], plan: plans[name] as { steps: [] } })
  })
  await page.route('**/api/plans/**', async (route) => {
    const req = route.request()
    const parts = new URL(req.url()).pathname.split('/')
    const id = parts[3]
    expect(req.headers()['x-nibfin-session']).toBe('SCREENSHOTSESSION')
    if (req.method() === 'DELETE') {
      api.deletes.push(id)
      return route.fulfill({ status: 204 })
    }
    if (parts[4] === 'code') {
      api.codes.push({ id, body: req.postDataJSON() })
      const status = api.codeStatus ?? 204
      if (status === 204) return route.fulfill({ status })
      return route.fulfill({ status, json: { error: { code: 'invalid_argument', message: 'no code requested' } } })
    }
    expect(parts[4]).toBe('apply')
    api.applies.push({ id, body: req.postDataJSON() })
    if (api.refuse) {
      const status = api.refuse
      api.refuse = undefined
      return route.fulfill({
        status,
        json: {
          error:
            status === 409
              ? { code: 'operation_running', message: 'another operation is running; wait for it to finish' }
              : { code: 'not_found', message: `no plan ${id}; plan again` },
        },
      })
    }
    const lines = applyLines[id] ?? loginLines[id]
    const cut = id === 'PLANINIT'
    return route.fulfill({ status: 200, contentType: 'application/x-ndjson', body: lines.join('\n') + (cut ? '' : '\n') })
  })
  return api
}

async function open(page: Page, width: number, theme: (typeof themes)[number] = 'dark') {
  await page.setViewportSize({ width, height: 900 })
  await page.addInitScript(([k, t]) => localStorage.setItem(k, t), [storageKey, theme])
  const w = await prepare(page)
  const api = await mockOps(page)
  const res = await page.goto('/operation.html#proof=SCREENSHOT')
  await expect(page.locator('main[data-session="open"]')).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  await expectNonce(page, res)
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  await expectNoPageScroll(page)
  return { w, api }
}

async function expectNoPageScroll(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
}

async function openDialog(page: Page, path: string): Promise<Locator> {
  await page.getByRole('button', { name: path, exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('heading', { level: 2 })).toHaveText(path)
  return dialog
}

async function expectNonceStyles(page: Page) {
  const styles = await page.evaluate(() => {
    const meta = document.querySelector<HTMLMetaElement>('meta[property=csp-nonce]')?.nonce
    return [...document.querySelectorAll('style')].map((s) => s.nonce === meta && !!meta)
  })
  expect(styles.length).toBeGreaterThan(0)
  expect(styles.every(Boolean)).toBe(true)
}

const item = (dialog: Locator, id: string) => dialog.locator(`li[data-item="${id}"]`)
const footer = (dialog: Locator) => dialog.locator('[data-slot=dialog-footer]')

// The commands without an api have no dialog; the CLI-only inputs have no field.
test('the harness lists only commands with an API name', async ({ page }) => {
  const { w } = await open(page, 1280)
  await expect(page.getByRole('button', { name: 'dashboard' })).toHaveCount(0)
  await expect(page.locator('main').getByRole('button')).toHaveCount(5)
  await expectClean(w)
})

for (const width of widths) {
  for (const theme of themes) {
    test(`operation dialog phases at ${width}px, ${theme}`, async ({ page }) => {
      const { w, api } = await open(page, width, theme)
      const sfx = `${theme}-${width}`

      // Inputs: one field per input with a request field, defaults set.
      let dialog = await openDialog(page, 'runner add')
      await expect(dialog.locator('[data-field]')).toHaveCount(2)
      await expect(dialog.getByLabel(/^count/)).toHaveValue('2')
      await expect(dialog.getByText('--yes')).toHaveCount(0)
      await expect(dialog.getByText('--json')).toHaveCount(0)
      await expect(dialog.getByRole('radiogroup', { name: /provider/ }).getByRole('radio')).toHaveCount(2)
      await expectNonceStyles(page)
      await shot(page, `operation-inputs-${sfx}`, false)

      // Field errors: nothing is sent.
      await dialog.getByLabel(/^count/).fill('two')
      await dialog.getByRole('button', { name: 'Preview' }).click()
      await expect(dialog.locator('[data-field=provider] [data-slot=field-error]')).toHaveText('Required.')
      await expect(dialog.locator('[data-field=count] [data-slot=field-error]')).toHaveText('Enter a whole number.')
      await expect(dialog.getByLabel(/^count/)).toHaveAttribute('aria-invalid', 'true')
      expect(api.plans).toEqual([])
      await expectNoPageScroll(page)
      await shot(page, `operation-input-errors-${sfx}`, false)

      // Preview: every step, no-change and after-start badges, notes.
      await dialog.getByRole('radio', { name: 'claude' }).click()
      await dialog.getByLabel(/^count/).fill('2')
      await dialog.getByRole('button', { name: 'Preview' }).click()
      await expect(dialog.getByRole('list', { name: 'Planned items' }).getByRole('listitem')).toHaveCount(4)
      expect(api.plans).toEqual([{ api: 'runner-add', body: { provider: 'claude', count: 2 } }])
      await expect(item(dialog, 'image').locator('[data-badge=no-change]')).toHaveText('No change needed')
      await expect(item(dialog, 'slots').locator('[data-badge=after-start]')).toHaveText('Known after the machine starts')
      await expect(item(dialog, 'machine').locator('[data-slot=transition]')).toContainText('stopped')
      await expect(item(dialog, 'login').locator('[data-slot=transition]')).toContainText('unknown')
      await expect(dialog.locator('[data-slot=plan-count]')).toContainText('3 items change, 1 already in place')
      await expect(dialog.getByRole('region', { name: 'Notes' })).toContainText('Runners pick up Issues')
      await expectNoPageScroll(page)
      await shot(page, `operation-preview-${sfx}`, false)

      // Progress: held mid-stream, one item in progress and one failed.
      await page.evaluate(() => (window as unknown as { __holdApply: (n: number) => void }).__holdApply(7))
      await dialog.getByRole('button', { name: 'Apply' }).click()
      await expect(item(dialog, 'slot:claude-4').locator('[data-badge=progress]')).toHaveText('In progress')
      await expect(item(dialog, 'machine').locator('[data-badge=progress]')).toHaveText('Done')
      await expect(item(dialog, 'slot:claude-3').locator('[data-badge=progress]')).toHaveText('Failed')
      await expect(item(dialog, 'slot:claude-3').locator('[data-slot=item-error]')).toHaveText(
        'runner_busy: slot claude-3 is still registering; run gh nibfin runner add again to finish it',
      )
      await expect(item(dialog, 'slots').locator('[data-badge=after-start]')).toBeVisible()
      await expect(item(dialog, 'slots').locator('[data-slot=placeholder]')).toContainText('Placeholder')
      await expect(dialog.getByRole('button', { name: 'Close' })).toHaveCount(0)
      expect(api.applies).toEqual([{ id: 'PLANADD', body: { confirm: true } }])
      // Escape and an outside click do not dismiss the running apply.
      await page.keyboard.press('Escape')
      await page.mouse.click(2, 2)
      await expect(dialog).toBeVisible()
      await expectNoPageScroll(page)
      await shot(page, `operation-progress-${sfx}`, false)

      // Read-back after an error with a result: partly not confirmed.
      await page.evaluate(() => (window as unknown as { __releaseApply: () => void }).__releaseApply())
      await expect(dialog.locator('[data-summary]')).toHaveAttribute('data-summary', 'notConfirmed')
      await expect(dialog.locator('[data-summary]')).toContainText('4 of 5')
      await expect(dialog.locator('[data-slot=final-error]')).toContainText('runner_busy: slot claude-3 is still registering')
      await expect(item(dialog, 'slot:claude-3').locator('[data-badge=readback]')).toHaveText('Not confirmed')
      await expect(item(dialog, 'slot:claude-3').locator('[data-slot=check]')).toContainText('the Runner is not online on GitHub yet')
      await expect(item(dialog, 'machine').locator('[data-badge=readback]')).toHaveText('Confirmed')
      await expect(item(dialog, 'slots').locator('[data-badge=readback]')).toHaveCount(0)
      await expect(item(dialog, 'slots').locator('[data-slot=placeholder]')).toBeVisible()
      await expectNoPageScroll(page)
      await shot(page, `operation-readback-partial-${sfx}`, false)
      await footer(dialog).getByRole('button', { name: 'Close' }).click()
      await expect(dialog).toBeHidden()
      await expect(page.locator('[data-finished="1"]')).toHaveCount(1)

      // Read-back all confirmed.
      dialog = await openDialog(page, 'runner remove')
      await expect(dialog.getByRole('checkbox', { name: /delete-login/ })).not.toBeChecked()
      await dialog.getByRole('radio', { name: 'claude' }).click()
      await dialog.getByRole('checkbox', { name: /delete-login/ }).check()
      await dialog.getByRole('button', { name: 'Preview' }).click()
      expect(api.plans.at(-1)).toEqual({ api: 'runner-remove', body: { provider: 'claude', count: 0, deleteLogin: true } })
      await dialog.getByRole('button', { name: 'Apply' }).click()
      await expect(dialog.locator('[data-summary]')).toHaveText('Confirmed: all 3 items were read back.')
      await expect(dialog.locator('[data-badge=readback]')).toHaveText(['Confirmed', 'Confirmed', 'Confirmed'])
      await shot(page, `operation-readback-confirmed-${sfx}`, false)
      await page.keyboard.press('Escape')
      await expect(dialog).toBeHidden()
      await expect(page.locator('[data-finished="2"]')).toHaveCount(1)

      // A cut stream: the outcome is unknown, nothing is confirmed.
      dialog = await openDialog(page, 'init')
      await dialog.getByRole('button', { name: 'Preview' }).click()
      expect(api.plans.at(-1)).toEqual({ api: 'init', body: {} })
      await dialog.getByRole('button', { name: 'Apply' }).click()
      await expect(dialog.locator('[data-summary]')).toHaveAttribute('data-summary', 'unknown')
      await expect(dialog.locator('[data-summary]')).toContainText('Read the status again')
      await expect(dialog.locator('[data-badge=readback]')).toHaveText(['Not read back', 'Not read back'])
      await expect(dialog.getByText('Confirmed', { exact: true })).toHaveCount(0)
      await shot(page, `operation-unknown-${sfx}`, false)
      await footer(dialog).getByRole('button', { name: 'Close' }).click()

      // A read-only plan: read back at once, no Apply.
      dialog = await openDialog(page, 'login')
      await dialog.getByRole('button', { name: 'Preview' }).click()
      expect(api.plans.at(-1)).toEqual({ api: 'login', body: {} })
      await expect(dialog.locator('[data-summary]')).toHaveText('Nothing to change. Confirmed: the item was read back.')
      await expect(dialog.getByRole('button', { name: 'Apply' })).toHaveCount(0)
      await expect(dialog.getByRole('region', { name: 'Notes' })).toContainText('Every provider is already logged in.')
      await shot(page, `operation-readonly-${sfx}`, false)
      await footer(dialog).getByRole('button', { name: 'Close' }).click()

      // A plan error keeps the inputs editable.
      dialog = await openDialog(page, 'upgrade')
      await dialog.getByRole('button', { name: 'Preview' }).click()
      await expect(dialog.locator('[data-slot=plan-error]')).toHaveText(
        'not_a_repository: the directory is not a Git repository; run it in a repository or set --repo',
      )
      await expect(dialog.getByLabel(/^repo/)).toBeEditable()
      await expectNoPageScroll(page)
      await shot(page, `operation-plan-error-${sfx}`, false)
      await dialog.getByRole('button', { name: 'Cancel' }).click()

      expect(api.applies.map((a) => a.id)).toEqual(['PLANADD', 'PLANREMOVE', 'PLANINIT'])
      expect(api.deletes).toEqual([])
      await expectClean(w, ['Failed to load resource: the server responded with a status of 422'])
    })
  }
}

const holdPlan = (page: Page, n: number) => page.evaluate((x) => (window as unknown as { __holdPlan: (n: number) => void }).__holdPlan(x), n)
const releasePlan = (page: Page) => page.evaluate(() => (window as unknown as { __releasePlan: () => void }).__releasePlan())
const planAborts = (page: Page) => page.evaluate(() => (window as unknown as { __aborts: number }).__aborts)

// Planning progress (#410): the items checked so far and the check running
// now, in the list the preview then completes.
for (const width of widths) {
  for (const theme of themes) {
    test(`planning progress at ${width}px, ${theme}`, async ({ page }) => {
      const { w } = await open(page, width, theme)
      const dialog = await openDialog(page, 'runner add')
      await dialog.getByRole('radio', { name: 'claude' }).click()
      // Two items checked; the third check runs.
      await holdPlan(page, 5)
      await dialog.getByRole('button', { name: 'Preview' }).click()
      const checked = dialog.getByRole('list', { name: 'Items checked so far' })
      await expect(checked.locator('li[data-item]')).toHaveCount(2)
      await expect(item(dialog, 'machine').locator('[data-slot=transition]')).toContainText('stopped')
      await expect(item(dialog, 'image').locator('[data-badge=no-change]')).toHaveText('No change needed')
      await expect(dialog.locator('li[data-slot=checking]')).toContainText('claude')
      await expect(dialog.locator('[data-badge=checking]')).toHaveText('Checking')
      await expect(dialog.locator('[data-slot=elapsed]')).toHaveText(/^\d+ s$/)
      await expect(footer(dialog).getByRole('button', { name: 'Cancel' })).toBeEnabled()
      await expect(footer(dialog).getByRole('button', { name: /Planning/ })).toBeDisabled()
      await expectNoPageScroll(page)
      await shot(page, `operation-planning-${theme}-${width}`, false)

      // The same items become the preview, in the plan's order.
      await releasePlan(page)
      const planned = dialog.getByRole('list', { name: 'Planned items' })
      await expect(planned.locator('li[data-item]')).toHaveCount(4)
      expect(await planned.locator('li[data-item]').evaluateAll((ls) => ls.map((l) => l.getAttribute('data-item')))).toEqual(['machine', 'image', 'login', 'slots'])
      await expect(dialog.locator('[data-slot=planning]')).toHaveCount(0)
      await expectClean(w)
    })
  }
}

test('Cancel or Escape while planning stops the planning and stores nothing', async ({ page }) => {
  const { w, api } = await open(page, 375)
  const dialog = await openDialog(page, 'init')
  await dialog.getByLabel(/^repo/).fill('acme/app')
  await holdPlan(page, 1)
  await dialog.getByRole('button', { name: 'Preview' }).click()
  await expect(dialog.locator('li[data-slot=checking]')).toContainText('acme/app scope')
  // The time so far counts up.
  await expect(dialog.locator('[data-slot=elapsed]')).toHaveText('1 s')
  const before = await planAborts(page)
  await footer(dialog).getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeHidden()
  await expect.poll(() => planAborts(page)).toBe(before + 1)
  await releasePlan(page)

  const again = await openDialog(page, 'init')
  await holdPlan(page, 1)
  await again.getByRole('button', { name: 'Preview' }).click()
  await expect(again.locator('li[data-slot=checking]')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(again).toBeHidden()
  await expect.poll(() => planAborts(page)).toBe(before + 2)
  await releasePlan(page)

  // A Preview after reopening starts fresh.
  const third = await openDialog(page, 'init')
  await third.getByRole('button', { name: 'Preview' }).click()
  await expect(third.getByRole('button', { name: 'Apply' })).toBeVisible()
  expect(api.plans).toHaveLength(3)
  expect(api.deletes).toEqual([])
  expect(api.applies).toEqual([])
  await expectClean(w)
})

test('Cancel in the preview drops the plan and applies nothing', async ({ page }) => {
  const { w, api } = await open(page, 375)
  const dialog = await openDialog(page, 'init')
  await dialog.getByLabel(/^repo/).fill('acme/app')
  await dialog.getByRole('button', { name: 'Preview' }).click()
  expect(api.plans).toEqual([{ api: 'init', body: { repo: 'acme/app' } }])
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeHidden()
  await expect.poll(() => api.deletes).toEqual(['PLANINIT'])
  expect(api.applies).toEqual([])
  await expect(page.locator('[data-finished="0"]')).toHaveCount(1)

  // Escape in the preview drops it too.
  const again = await openDialog(page, 'init')
  await again.getByRole('button', { name: 'Preview' }).click()
  await expect(again.getByRole('button', { name: 'Apply' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect.poll(() => api.deletes).toEqual(['PLANINIT', 'PLANINIT'])
  expect(api.applies).toEqual([])
  await expectClean(w)
})

test('a refused apply can be retried after 409 and only closed after 404', async ({ page }) => {
  const { w, api } = await open(page, 768, 'light')
  let dialog = await openDialog(page, 'init')
  await dialog.getByRole('button', { name: 'Preview' }).click()
  api.refuse = 409
  await dialog.getByRole('button', { name: 'Apply' }).click()
  await expect(dialog.locator('[data-slot=refused]')).toHaveText('operation_running: another operation is running; wait for it to finish')
  await shot(page, 'operation-refused-light-768', false)
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect.poll(() => api.deletes).toEqual(['PLANINIT'])

  dialog = await openDialog(page, 'init')
  await dialog.getByRole('button', { name: 'Preview' }).click()
  api.refuse = 404
  await dialog.getByRole('button', { name: 'Apply' }).click()
  await expect(dialog.locator('[data-slot=refused]')).toContainText('not_found')
  await expect(dialog.getByRole('button', { name: 'Apply' })).toHaveCount(0)
  await footer(dialog).getByRole('button', { name: 'Close' }).click()
  await expect(dialog).toBeHidden()
  expect(api.deletes).toEqual(['PLANINIT'])
  await expectClean(w, [
    'Failed to load resource: the server responded with a status of 409',
    'Failed to load resource: the server responded with a status of 404',
  ])
})

test('a read-only plan whose read-back failed shows the error next to the items', async ({ page }) => {
  const { w, api } = await open(page, 1280)
  const dialog = await openDialog(page, 'upgrade')
  await dialog.getByLabel(/^repo/).fill('acme/broken')
  await dialog.getByRole('button', { name: 'Preview' }).click()
  expect(api.plans).toEqual([{ api: 'upgrade', body: { repo: 'acme/broken' } }])
  await expect(dialog.locator('[data-summary]')).toHaveAttribute('data-summary', 'notConfirmed')
  await expect(dialog.locator('[data-slot=final-error]')).toHaveText('github_error: reading the workflows of acme/broken: HTTP 502')
  await expect(dialog.locator('[data-badge=readback]')).toHaveText('Not confirmed')
  await shot(page, 'operation-readback-failed-1280', false)
  await expectClean(w, ['Failed to load resource: the server responded with a status of 422'])
})

test('the in-progress icon does not spin when reduced motion is requested', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const { w } = await open(page, 1280)
  const dialog = await openDialog(page, 'runner add')
  await dialog.getByRole('radio', { name: 'claude' }).click()
  await dialog.getByRole('button', { name: 'Preview' }).click()
  await page.evaluate(() => (window as unknown as { __holdApply: (n: number) => void }).__holdApply(3))
  await dialog.getByRole('button', { name: 'Apply' }).click()
  const badge = item(dialog, 'login').locator('[data-badge=progress]')
  await expect(badge).toHaveText('In progress')
  expect(await badge.locator('svg').evaluate((s) => getComputedStyle(s).animationName)).toBe('none')
  await page.evaluate(() => (window as unknown as { __releaseApply: () => void }).__releaseApply())
  await expect(dialog.locator('[data-summary]')).toBeVisible()
  await expectClean(w)
})

// The login steps (#354) in the dialog.

const hold = (page: Page, n: number) => page.evaluate((x) => (window as unknown as { __holdApply: (n: number) => void }).__holdApply(x), n)
const release = (page: Page) => page.evaluate(() => (window as unknown as { __releaseApply: () => void }).__releaseApply())
const aborts = (page: Page) => page.evaluate(() => (window as unknown as { __aborts: number }).__aborts)
const panel = (dialog: Locator, provider: string) => dialog.locator(`section[data-login="${provider}"]`)
const currentStep = (dialog: Locator, provider: string) => panel(dialog, provider).locator('[aria-current=step]')

async function startLogin(page: Page, path: string, provider: string, held?: number): Promise<Locator> {
  const dialog = await openDialog(page, path)
  await dialog.getByRole('radio', { name: provider }).click()
  await dialog.getByRole('button', { name: 'Preview' }).click()
  await expect(dialog.getByRole('button', { name: 'Apply' })).toBeVisible()
  if (held !== undefined) await hold(page, held)
  await dialog.getByRole('button', { name: 'Apply' }).click()
  return dialog
}

for (const width of widths) {
  for (const theme of themes) {
    test(`login steps at ${width}px, ${theme}`, async ({ page }) => {
      const { w, api } = await open(page, width, theme)
      const sfx = `${theme}-${width}`

      // Claude: the URL, Open and the code field while waiting.
      let dialog = await startLogin(page, 'login', 'claude', 3)
      const claude = panel(dialog, 'claude')
      await expect(currentStep(dialog, 'claude')).toContainText('Waiting for authorization')
      await expect(claude.locator('[data-slot=login-url]')).toHaveText(/^https:\/\/claude\.ai\/oauth\/authorize\?/)
      const link = claude.getByRole('link', { name: 'Open sign-in page' })
      await expect(link).toHaveAttribute('href', /^https:\/\/claude\.ai\/oauth\/authorize\?/)
      await expect(link).toHaveAttribute('target', '_blank')
      await expect(link).toHaveAttribute('rel', 'noreferrer noopener')
      const field = claude.getByLabel('Authorization code')
      await expect(field).toBeEditable()
      await expect(field).toHaveAttribute('autocomplete', 'off')
      await expect(claude.locator('[data-slot=login-timeout]')).toContainText('if not authorized')
      await expect(dialog.getByRole('button', { name: 'End login' })).toBeVisible()
      await expect(dialog.locator('[data-slot=end-login-note]')).toHaveText('Ending the login cancels this login; run it again to continue.')
      await expect(page.getByRole('button', { name: 'Close' })).toBeVisible()
      await expectNonceStyles(page)
      await expectNoPageScroll(page)
      await shot(page, `login-claude-waiting-${sfx}`, false)

      // A refused code shows its error and keeps the field.
      api.codeStatus = 409
      await field.fill('FICTIONAL-CODE-0000')
      await claude.getByRole('button', { name: 'Submit code' }).click()
      await expect(claude.locator('[data-slot=code-error]')).toHaveText('invalid_argument: no code requested')
      await expect(field).toBeEditable()
      await shot(page, `login-code-refused-${sfx}`, false)

      // The code goes once; then Verifying, and the field is cleared.
      api.codeStatus = 204
      await claude.getByRole('button', { name: 'Submit code' }).click()
      await expect(currentStep(dialog, 'claude')).toContainText('Verifying')
      await expect(field).toBeDisabled()
      await expect(field).toHaveValue('')
      expect(api.codes).toEqual([
        { id: 'PLANLOGINCLAUDE', body: { code: 'FICTIONAL-CODE-0000' } },
        { id: 'PLANLOGINCLAUDE', body: { code: 'FICTIONAL-CODE-0000' } },
      ])
      // Outside a login wait the running apply cannot be dismissed.
      await expect(dialog.getByRole('button', { name: 'End login' })).toHaveCount(0)
      await page.keyboard.press('Escape')
      await expect(dialog).toBeVisible()
      await expectNoPageScroll(page)
      await shot(page, `login-verifying-${sfx}`, false)

      // Done only after the read-back.
      await release(page)
      await expect(dialog.locator('[data-summary]')).toHaveAttribute('data-summary', 'confirmed')
      await expect(claude).toHaveAttribute('data-stage', 'done')
      await expect(currentStep(dialog, 'claude')).toContainText('Done')
      await expect(claude.locator('[data-slot=login-message]')).toContainText('confirmed by the read-back')
      await expectNoPageScroll(page)
      await shot(page, `login-done-${sfx}`, false)
      await footer(dialog).getByRole('button', { name: 'Close' }).click()
      await expect(dialog).toBeHidden()

      // Codex: the URL and the one-time code with Copy, and no input.
      dialog = await startLogin(page, 'login', 'codex', 3)
      const codex = panel(dialog, 'codex')
      await expect(codex.locator('[data-slot=device-code]')).toHaveText('WXYZ-7K3Q9')
      await expect(codex.getByRole('link', { name: 'Open sign-in page' })).toHaveAttribute('href', 'https://auth.openai.com/codex/device')
      await expect(codex.getByRole('textbox')).toHaveCount(0)
      await codex.getByRole('button', { name: /Copy code/ }).click()
      await expect(codex.locator('[data-action=copy-code], [data-slot=copy-state]').filter({ hasText: /Copied|Copy failed: select the code/ })).toHaveCount(1)
      await expectNoPageScroll(page)
      await shot(page, `login-codex-waiting-${sfx}`, false)
      await hold(page, 4)
      await expect(currentStep(dialog, 'codex')).toContainText('Verifying')
      await expect(codex.locator('[data-slot=device-code]')).toHaveCount(0)
      await release(page)
      await expect(codex).toHaveAttribute('data-stage', 'done')
      await shot(page, `login-codex-done-${sfx}`, false)
      await footer(dialog).getByRole('button', { name: 'Close' }).click()

      // runner add logs codex in with the same panel; the CLI fails.
      dialog = await startLogin(page, 'runner add', 'codex')
      await expect(panel(dialog, 'codex')).toHaveAttribute('data-stage', 'failed')
      await expect(panel(dialog, 'codex').locator('[data-slot=login-message]')).toHaveText('Failed: codex login exited with status 1')
      await expect(dialog.locator('[data-summary]')).toHaveAttribute('data-summary', 'notConfirmed')
      await expect(dialog.getByText('Confirmed', { exact: true })).toHaveCount(0)
      await expectNoPageScroll(page)
      await shot(page, `login-failed-${sfx}`, false)
      await footer(dialog).getByRole('button', { name: 'Close' }).click()

      // A busy Runner's removal waits for its Job: the item stays in progress.
      dialog = await openDialog(page, 'runner remove')
      await dialog.getByRole('radio', { name: 'codex' }).click()
      await dialog.getByRole('button', { name: 'Preview' }).click()
      await expect(item(dialog, 'slot:codex-1')).toContainText('busy: waits for its Job to end')
      await hold(page, 1)
      await dialog.getByRole('button', { name: 'Apply' }).click()
      await expect(item(dialog, 'slot:codex-1').locator('[data-badge=progress]')).toHaveText('In progress')
      await expect(item(dialog, 'slot:codex-1')).toContainText('busy: waits for its Job to end')
      await expect(page.getByRole('button', { name: 'Close' })).toHaveCount(0)
      await expectNoPageScroll(page)
      await shot(page, `remove-busy-${sfx}`, false)
      await release(page)
      await expect(dialog.locator('[data-summary]')).toHaveText('Confirmed: the item was read back.')
      await footer(dialog).getByRole('button', { name: 'Close' }).click()

      // End login during the wait: the request is aborted, nothing confirmed.
      dialog = await startLogin(page, 'login', 'claude', 3)
      await expect(currentStep(dialog, 'claude')).toContainText('Waiting for authorization')
      await dialog.getByRole('button', { name: 'End login' }).click()
      await expect(dialog.locator('[data-summary]')).toHaveAttribute('data-summary', 'ended')
      await expect(dialog.locator('[data-summary]')).toContainText('The login was ended (you chose End login).')
      await expect(panel(dialog, 'claude')).toHaveAttribute('data-stage', 'ended')
      await expect(dialog.locator('[data-badge=readback]')).toHaveText(['Not read back', 'Not read back', 'Not read back'])
      expect(await aborts(page)).toBe(1)
      await expectNoPageScroll(page)
      await shot(page, `login-ended-${sfx}`, false)
      await footer(dialog).getByRole('button', { name: 'Close' }).click()

      expect(api.applies.map((a) => a.id)).toEqual(['PLANLOGINCLAUDE', 'PLANLOGINCODEX', 'PLANADDCODEX', 'PLANREMOVEBUSY', 'PLANLOGINCLAUDE'])
      await expectClean(w, ['Failed to load resource: the server responded with a status of 409'])
    })
  }
}

test('Escape during a login wait ends the request and closes the dialog', async ({ page }) => {
  const { w, api } = await open(page, 375)
  const dialog = await startLogin(page, 'login', 'codex', 3)
  await expect(panel(dialog, 'codex').locator('[data-slot=device-code]')).toBeVisible()
  // An outside click stays blocked.
  await page.mouse.click(2, 2)
  await expect(dialog).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect.poll(() => aborts(page)).toBe(1)
  await expect(page.locator('[data-finished="1"]')).toHaveCount(1)
  expect(api.applies.map((a) => a.id)).toEqual(['PLANLOGINCODEX'])
  expect(api.codes).toEqual([])
  await expectClean(w)
})

test('the close button during a login wait ends the request', async ({ page }) => {
  const { w, api } = await open(page, 1280, 'light')
  const dialog = await startLogin(page, 'login', 'claude', 3)
  await expect(currentStep(dialog, 'claude')).toContainText('Waiting for authorization')
  await page.getByRole('button', { name: 'Close' }).click()
  await expect(dialog).toBeHidden()
  await expect.poll(() => aborts(page)).toBe(1)
  expect(api.applies).toHaveLength(1)
  await expectClean(w)
})

for (const theme of themes) {
  test(`a login wait times out after 15 minutes, ${theme}`, async ({ page }) => {
    await page.clock.install()
    const { w, api } = await open(page, 768, theme)
    const dialog = await startLogin(page, 'login', 'claude', 3)
    await expect(currentStep(dialog, 'claude')).toContainText('Waiting for authorization')
    await page.clock.fastForward('14:59')
    await expect(dialog.locator('[data-summary]')).toHaveCount(0)
    await page.clock.fastForward('00:01')
    await expect(dialog.locator('[data-summary]')).toContainText('The login was ended (timed out after 15 minutes).')
    await expect(panel(dialog, 'claude').locator('[data-slot=login-message]')).toHaveText('Ended: timed out after 15 minutes')
    expect(await aborts(page)).toBe(1)
    expect(api.applies).toHaveLength(1)
    await expectNoPageScroll(page)
    await shot(page, `login-timeout-${theme}-768`, false)
    await expectClean(w)
  })
}
