import { expect, test, type Locator, type Page } from '@playwright/test'

import { expectClean, expectNonce, prepare, shot, storageKey, widths } from './support'

// Screenshots and checks of the operation dialog (#351) in its harness
// (operation.html), served by the real Go server with its CSP. The schema,
// plan, apply and delete routes are mocked with fictional bodies.

const themes = ['dark', 'light'] as const

const providerArg = { name: 'provider', required: true, repeatable: false, allowed: ['claude', 'codex'], param: 'provider' }
const yes = { name: 'yes', shorthand: 'y', type: 'bool', default: 'false', usage: 'apply without asking for confirmation', required: false }
const json = { name: 'json', shorthand: '', type: 'bool', default: 'false', usage: 'print the result as JSON', required: false }
const repo = {
  name: 'repo',
  shorthand: 'R',
  type: 'string',
  default: '',
  usage: 'the repository as [HOST/]OWNER/REPO (default: the current directory)',
  required: false,
  param: 'repo',
}

const commands = [
  {
    path: 'runner add',
    api: 'runner-add',
    short: 'Add Runners for a provider on this host',
    long: 'Add Runners for a provider on this host.\n\nThe machine starts when it is stopped, and the provider login is created when it is missing.',
    flags: [{ name: 'count', shorthand: '', type: 'int', default: '2', usage: 'number of Runners', required: false, param: 'count' }, yes, json],
    args: [providerArg],
  },
  {
    path: 'runner remove',
    api: 'runner-remove',
    short: 'Remove Runners for a provider on this host',
    long: '',
    flags: [
      { name: 'count', shorthand: '', type: 'int', default: '0', usage: 'number of Runners (default: all)', required: false, param: 'count' },
      {
        name: 'delete-login',
        shorthand: '',
        type: 'bool',
        default: 'false',
        usage: 'also delete the provider login when no Runner of it remains',
        required: false,
        param: 'deleteLogin',
      },
      yes,
      json,
    ],
    args: [providerArg],
  },
  {
    path: 'login',
    api: 'login',
    short: 'Log in to a provider for the Runners',
    long: '',
    flags: [json],
    args: [{ ...providerArg, required: false }],
  },
  { path: 'init', api: 'init', short: 'Set up a repository for Nibfin', long: '', flags: [repo, yes, json], args: [] },
  { path: 'upgrade', api: 'upgrade', short: 'Upgrade the Nibfin workflows of a repository', long: '', flags: [repo, yes, json], args: [] },
  {
    path: 'dashboard',
    short: 'Start the local Dashboard',
    long: '',
    flags: [{ name: 'port', shorthand: '', type: 'int', default: '0', usage: 'port', required: false }],
    args: [],
  },
]

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

interface Api {
  plans: { api: string; body: unknown }[]
  applies: { id: string; body: unknown }[]
  deletes: string[]
  // refuse, when set, answers the next apply with this status.
  refuse?: number
}

async function mockOps(page: Page): Promise<Api> {
  const api: Api = { plans: [], applies: [], deletes: [] }
  await page.route('**/api/commands', (route) => {
    expect(route.request().method()).toBe('GET')
    expect(route.request().headers()['x-nibfin-session']).toBe('SCREENSHOTSESSION')
    return route.fulfill({ json: { commands } })
  })
  await page.route('**/api/commands/*/plan', (route) => {
    const name = new URL(route.request().url()).pathname.split('/')[3]
    expect(route.request().headers()['x-nibfin-session']).toBe('SCREENSHOTSESSION')
    const body = route.request().postDataJSON() as Record<string, unknown>
    api.plans.push({ api: name, body })
    if (name === 'login') {
      return route.fulfill({
        json: {
          plan: { operation: 'login', steps: [step('login:claude', 'log in', 'claude', { current: 'logged in', next: 'logged in', noChange: true })], confirm: false, readOnly: true },
          result: { items: [check('login:claude', 'log in', 'claude', true, 'logged in')], notes: ['Every provider is already logged in.'] },
        },
      })
    }
    if (name === 'upgrade') {
      if (body.repo === 'acme/broken') {
        return route.fulfill({
          status: 422,
          json: {
            error: { code: 'github_error', message: 'reading the workflows of acme/broken: HTTP 502' },
            plan: { operation: 'upgrade', repo: 'acme/broken', steps: [step('workflow', 'keep workflow', '.github/workflows/nibfin.yml', { noChange: true })], confirm: false, readOnly: true },
            result: { items: [check('workflow', 'keep workflow', '.github/workflows/nibfin.yml', false, '', 'the read failed: HTTP 502')] },
          },
        })
      }
      return route.fulfill({
        status: 422,
        json: { error: { code: 'not_a_repository', message: 'the directory is not a Git repository; run it in a repository or set --repo' } },
      })
    }
    return route.fulfill({ json: { id: ids[name], plan: plans[name] } })
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
    const lines = applyLines[id]
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
