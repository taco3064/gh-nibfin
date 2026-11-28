import { expect, test, type Page, type Route } from '@playwright/test'

import { commands, resumeCommand } from './command-fixtures'
import { expectClean, expectNonce, fulfillPlanStream, prepare, shot, storageKey, widths, type PlanAnswer } from './support'

// Screenshots and checks of the repair states and the Repair dialog
// (Proposal R9.52–R9.56, #509) in the Issue element harness
// (screenshots.html), served by the real Go server with its CSP. The
// schema, plan, apply and delete routes are mocked with fictional bodies.

const gh = 'https://github.com'
const url = (repo: string, n: number) => `${gh}/${repo}/issues/${n}`
const themes = ['dark', 'light'] as const
const choose = url('acme/app', 108)

const step = (id: string, action: string, target: string, extra: Record<string, unknown> = {}) => ({
  id,
  action,
  target,
  noChange: false,
  afterStart: false,
  ...extra,
})

interface PlanBody {
  issues: string[]
  option?: number
  reply?: string
}

// plan answers like resume: a reply's comment item shows its exact body.
function plan(body: PlanBody): PlanAnswer {
  const comment =
    body.reply !== undefined
      ? step(`decision:${choose}`, 'add comment', choose, { detail: body.reply, current: 'no decision', next: 'a reply by octo-dev' })
      : step(`decision:${choose}`, 'add comment', choose, { detail: `decision for option ${body.option}`, next: `option ${body.option} by octo-dev` })
  return {
    id: 'PLANREPAIR',
    plan: {
      operation: 'resume',
      repo: 'github.com/acme/app',
      steps: [
        comment,
        step(`label:${choose}:pending`, 'remove label', `${choose} pending`, { current: 'present', next: 'absent' }),
      ],
      confirm: false,
      readOnly: false,
    },
  }
}

const progress = (id: string, action: string, target: string, phase: string) =>
  JSON.stringify({ event: 'progress', progress: { id, action, target, phase } })

type ApplyMode = 'confirmed' | 'newer'

interface Api {
  plans: unknown[]
  applies: { id: string; body: unknown }[]
  deletes: string[]
  apply: ApplyMode
  planError?: { code: string; message: string }
}

function applyLines(mode: ApplyMode, body: PlanBody): string[] {
  const mine = body.reply !== undefined ? 'a reply by octo-dev' : `option ${body.option} by octo-dev`
  const comment = {
    id: `decision:${choose}`,
    action: 'add comment',
    target: choose,
    confirmed: mode === 'confirmed',
    state: mode === 'confirmed' ? mine : 'option 1 by other-dev',
    ...(mode === 'newer' ? { reason: 'the newest decision is option 1 by other-dev' } : {}),
  }
  const label = { id: `label:${choose}:pending`, action: 'remove label', target: `${choose} pending`, confirmed: true, state: 'absent' }
  const done = [
    progress(`decision:${choose}`, 'add comment', choose, 'started'),
    progress(`decision:${choose}`, 'add comment', choose, 'done'),
    progress(`label:${choose}:pending`, 'remove label', `${choose} pending`, 'started'),
    progress(`label:${choose}:pending`, 'remove label', `${choose} pending`, 'done'),
  ]
  if (mode === 'confirmed') return [...done, JSON.stringify({ event: 'result', result: { items: [comment, label], notes: [] } })]
  return [
    ...done,
    JSON.stringify({
      event: 'error',
      error: {
        code: 'not_applied',
        message: `resume: your decision on ${choose} is not the newest decision: the newest decision is option 1 by other-dev`,
      },
      result: { items: [comment, label], notes: [] },
    }),
  ]
}

async function mock(page: Page): Promise<Api> {
  const api: Api = { plans: [], applies: [], deletes: [], apply: 'confirmed' }
  await page.route('**/api/commands', (route) => route.fulfill({ json: { commands: [...commands, resumeCommand] } }))
  await page.route('**/api/commands/resume/plan/stream', async (route: Route) => {
    expect(route.request().headers()['x-nibfin-session']).toBe('SCREENSHOTSESSION')
    const body = route.request().postDataJSON() as PlanBody
    api.plans.push(body)
    if (api.planError) return fulfillPlanStream(route, { error: api.planError })
    return fulfillPlanStream(route, plan(body))
  })
  await page.route('**/api/plans/**', async (route) => {
    const req = route.request()
    const id = new URL(req.url()).pathname.split('/')[3]
    if (req.method() === 'DELETE') {
      api.deletes.push(id)
      return route.fulfill({ status: 204 })
    }
    api.applies.push({ id, body: req.postDataJSON() })
    await route.fulfill({ status: 200, contentType: 'application/x-ndjson', body: applyLines(api.apply, api.plans.at(-1) as PlanBody).join('\n') + '\n' })
  })
  return api
}

async function open(page: Page, width: number, theme: (typeof themes)[number] = 'dark') {
  await page.setViewportSize({ width, height: 900 })
  await page.addInitScript(([k, t]) => localStorage.setItem(k, t), [storageKey, theme])
  const w = await prepare(page)
  const api = await mock(page)
  const res = await page.goto('/screenshots.html#proof=SCREENSHOT')
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

const item = (page: Page, repo: string, n: number) => page.locator(`article[data-issue="${url(repo, n)}"]`)

async function openRepair(page: Page, repo: string, n: number) {
  const button = item(page, repo, n).getByRole('button', { name: 'Repair' })
  await expect(button).toBeEnabled()
  await button.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  return { button, dialog }
}

for (const width of widths) {
  for (const theme of themes) {
    test(`repair states and the Repair dialog at ${width}px, ${theme}`, async ({ page }) => {
      const { w, api } = await open(page, width, theme)

      // Each state is a badge with its one-line reason.
      const states: [string, number, string, string][] = [
        ['acme/app', 107, 'Repairing', 'CI failed on a flaky integration test; the repair stage reruns CI.'],
        ['acme/app', 108, 'Waiting for your choice', 'The Shape and the Issue disagree'],
        ['acme/app', 109, 'Needs your action', 'The Runner has no provider login, so no agent could start.'],
        ['acme/docs', 8, 'Needs your action', 'No repair record covers this stop yet; see the pending comment'],
      ]
      for (const [repo, n, label, reason] of states) {
        await expect(item(page, repo, n).locator('[data-badge=pending] [data-repair]')).toHaveText(label)
        await expect(item(page, repo, n).locator('[data-slot=repair-line]')).toContainText(reason)
      }
      // Repairing needs no person: no Repair button. Without a read repair
      // state the element shows Pending as before.
      await expect(item(page, 'acme/app', 107).getByRole('button', { name: 'Repair' })).toHaveCount(0)
      await expect(item(page, 'acme/app', 103).getByRole('button', { name: 'Repair' })).toHaveCount(0)
      await expect(item(page, 'acme/app', 103).locator('[data-badge=pending]')).toContainText('Pending')
      // The same permission rule as pause and resume.
      const docs = item(page, 'acme/docs', 8)
      const reason = 'Needs triage permission or higher; this account has READ on acme/docs.'
      await expect(docs.getByRole('button', { name: 'Repair' })).toBeDisabled()
      await expect(docs.getByRole('button', { name: 'Repair' })).toHaveAccessibleDescription(reason)
      await expect(docs.locator('[data-slot=disabled-reason]')).toHaveText(reason)
      // What a decision left for a person shows on the Issue item, as text,
      // with a link to Core's comment.
      const infra = item(page, 'acme/infra', 12).locator('[data-slot=needs-person]')
      await expect(infra).toContainText('Needs a person: Nibfin did not do these')
      await expect(infra.locator('[data-slot=needs-person-item]')).toHaveText([
        'Add the `pause` label until the bucket policy is reviewed.',
        'Rerun the failed CI job on the default branch.',
      ])
      await expect(infra.getByRole('link')).toHaveAttribute('href', `${gh}/acme/infra/issues/12#issuecomment-6101`)
      await expect(item(page, 'acme/app', 108).locator('[data-slot=needs-person-item]')).toHaveText('Close the duplicate Issue acme/app#97.')
      await expect(item(page, 'acme/app', 107).locator('[data-slot=needs-person]')).toHaveCount(0)
      await item(page, 'acme/app', 108).scrollIntoViewIfNeeded()
      await shot(page, `repair-states-${theme}-${width}`)

      // Waiting for your choice: the options as suggestions, the
      // recommended one chosen, and a reply in your own words.
      let { button, dialog } = await openRepair(page, 'acme/app', 108)
      await expect(dialog.getByRole('heading', { level: 2 })).toHaveText('Repair #108')
      const head = dialog.locator('[data-slot=repair-summary]')
      await expect(head.locator('[data-repair]')).toHaveText('Waiting for your choice')
      await expect(head.locator('[data-slot=repair-category]')).toHaveText('Requirement conflict')
      await expect(head.locator('[data-slot=repair-done]')).toContainText('Compared the Shape')
      await expect(head.getByRole('link', { name: /Repair record/ })).toHaveAttribute('href', `${choose}#issuecomment-5202`)
      await expect(head.getByRole('link', { name: /Pending comment/ })).toHaveAttribute('href', `${choose}#issuecomment-5201`)
      await expect(head.getByRole('link', { name: /Latest run/ })).toHaveAttribute('href', `${gh}/acme/nibfin-exec/actions/runs/7108`)
      const radios = dialog.getByRole('radio')
      await expect(radios).toHaveCount(3)
      await expect(dialog.getByRole('radio', { checked: true })).toHaveCount(1)
      await expect(radios.nth(1)).toBeChecked()
      await expect(dialog.locator('[data-option="2"] [data-slot=recommended]')).toHaveText('Recommended')
      await expect(dialog.locator('[data-option="2"]')).toContainText('Nibfin changes the Issue: adds a clause')
      await expect(dialog.locator('[data-option="2"] [data-slot=consequence]')).toContainText('What happens next')
      // Every card says Nibfin changes only the Issue text and Blocked by,
      // and that any other step is the person's.
      const boundary = dialog.locator('[data-slot=boundary]')
      await expect(boundary).toHaveCount(3)
      for (let n = 0; n < 3; n++) {
        await expect(boundary.nth(n)).toBeVisible()
        await expect(boundary.nth(n)).toContainText('lets Nibfin change only the Issue text and Blocked by')
        await expect(boundary.nth(n)).toContainText('adding a label, running CI, closing the Issue or opening a PR, is yours to do')
      }
      await expect(head.locator('[data-slot=needs-person-item]')).toHaveText('Close the duplicate Issue acme/app#97.')
      const reply = dialog.getByRole('textbox', { name: 'Reply in your own words' })
      await expect(reply).toHaveCount(1)
      await expect(reply).toHaveValue('')
      await expect(dialog.getByText('--repo')).toHaveCount(0)
      await expect(dialog.getByRole('button', { name: 'Submit' })).toBeEnabled()
      await expectNoPageScroll(page)
      await shot(page, `repair-choose-${theme}-${width}`, false)

      // Typing a reply unchecks the cards and turns the send action into
      // the reply, which stops at the preview of the exact comment.
      const text = 'Keep option 2, but migrate the old attachments in a later Issue.\nLeave the bucket policy as it is.'
      await reply.fill(text)
      await expect(dialog.getByRole('radio', { checked: true })).toHaveCount(0)
      await expect(dialog.locator('[data-field=reply]')).toHaveAttribute('data-replying', 'true')
      await expect(dialog.getByRole('button', { name: 'Submit' })).toHaveCount(0)
      await expect(dialog.getByRole('button', { name: 'Preview reply' })).toBeEnabled()
      await expectNoPageScroll(page)
      await shot(page, `repair-reply-${theme}-${width}`, false)
      await dialog.getByRole('button', { name: 'Preview reply' }).click()
      await expect(dialog).toHaveAttribute('data-phase', 'preview')
      expect(api.plans).toEqual([{ issues: [choose], reply: text }])
      const comment = dialog.locator(`[data-item="decision:${choose}"]`)
      await expect(comment).toContainText('add comment')
      await expect(comment).toContainText('a reply by octo-dev')
      await expect(comment.locator('[data-slot=item-detail]')).toBeVisible()
      // The preview shows the exact comment, its line breaks kept.
      expect(await comment.locator('[data-slot=item-detail]').innerText()).toBe(text)
      await expect(dialog.getByRole('button', { name: 'Send reply' })).toBeEnabled()
      expect(api.applies).toEqual([])
      await expectNoPageScroll(page)
      await shot(page, `repair-reply-preview-${theme}-${width}`, false)
      await dialog.getByRole('button', { name: 'Cancel' }).click()
      await expect(dialog).toBeHidden()
      await expect(button).toBeFocused()
      expect(api.deletes).toEqual(['PLANREPAIR'])
      api.plans.length = 0
      api.deletes.length = 0

      // Needs your action: the advice only, nothing runs.
      ;({ button, dialog } = await openRepair(page, 'acme/app', 109))
      await expect(dialog.getByRole('heading', { level: 2 })).toHaveText('Repair #109')
      await expect(dialog.locator('[data-slot=repair-advice]')).toContainText('Log in to the provider on the runner host')
      const steps = dialog.locator('[data-slot=repair-steps] li')
      await expect(steps).toHaveCount(3)
      await expect(steps.nth(0).getByRole('link')).toHaveAttribute('href', `${gh}/acme/app/actions/runs/9050`)
      await expect(steps.nth(1).locator('code')).toHaveText('gh nibfin login claude')
      // A link that is not http(s) is text, not a link.
      await expect(steps.nth(2).getByRole('link')).toHaveCount(0)
      await expect(steps.nth(2)).toContainText('javascript:alert(1)')
      await expect(dialog.locator('[data-slot=repair-bug]').getByRole('link')).toHaveAttribute('href', `${gh}/acme/nibfin/issues/new`)
      await expect(dialog.locator('[data-slot=repair-bug] code')).toContainText('Expected: the Runner is not offered Jobs')
      await expect(dialog.locator('[data-slot=repair-links]')).toContainText('Runs not read')
      await expect(dialog.locator('[data-slot=repair-resume-note]')).toContainText('use Resume on the Issue to remove pending')
      await expect(dialog.getByRole('button', { name: /^Resume/ })).toHaveCount(0)
      await expectNoPageScroll(page)
      await shot(page, `repair-act-${theme}-${width}`, false)
      await dialog.getByRole('button', { name: 'Close' }).first().click()
      await expect(dialog).toBeHidden()
      await expect(button).toBeFocused()

      expect(api.plans).toEqual([])
      expect(api.applies).toEqual([])
      await expectClean(w)
    })
  }
}

test('choosing an option plans and applies resume --option at once and reads it back', async ({ page }) => {
  const { w, api } = await open(page, 1280)
  const { dialog } = await openRepair(page, 'acme/app', 108)
  await dialog.getByRole('radio', { name: /Stop and split the Issue/ }).check()
  await dialog.getByRole('button', { name: 'Submit' }).click()

  await expect(dialog.locator('[data-summary]')).toHaveAttribute('data-summary', 'confirmed')
  expect(api.plans).toEqual([{ issues: [choose], option: 3 }])
  // No preview stop: the stored plan was applied once, confirmed by the
  // Dashboard as resume is.
  expect(api.applies).toEqual([{ id: 'PLANREPAIR', body: { confirm: true } }])
  expect(api.deletes).toEqual([])
  await expect(dialog.locator('[data-slot=repair-summary]')).toBeVisible()
  await expect(dialog).toContainText('option 3 by octo-dev')
  await shot(page, 'repair-choose-confirmed-1280', false)
  await dialog.getByRole('button', { name: 'Close' }).first().click()
  await expect(dialog).toBeHidden()
  // Closing after the apply asks the view to read the status again.
  await expect(page.locator('[data-rereads="1"]')).toHaveCount(1)
  await expectClean(w)
})

test('a newer decision by someone else is not confirmed', async ({ page }) => {
  const { w, api } = await open(page, 375, 'light')
  api.apply = 'newer'
  const { dialog } = await openRepair(page, 'acme/app', 108)
  await dialog.getByRole('button', { name: 'Submit' }).click()
  await expect(dialog.locator('[data-summary]')).toHaveAttribute('data-summary', 'notConfirmed')
  await expect(dialog.locator('[data-slot=final-error]')).toContainText('not_applied')
  await expect(dialog.locator('[data-slot=final-error]')).toContainText('the newest decision is option 1 by other-dev')
  expect(api.plans).toEqual([{ issues: [choose], option: 2 }])
  await expectNoPageScroll(page)
  await shot(page, 'repair-choose-not-confirmed-light-375', false)
  await expectClean(w)
})

test('a refused plan shows its code and message and applies nothing', async ({ page }) => {
  const { w, api } = await open(page, 768)
  api.planError = { code: 'invalid_argument', message: `the repair record of ${choose} is no longer waiting for a choice; run gh nibfin status again` }
  const { dialog } = await openRepair(page, 'acme/app', 108)
  await dialog.getByRole('button', { name: 'Submit' }).click()
  await expect(dialog.locator('[data-slot=plan-error]')).toContainText('invalid_argument')
  await expect(dialog.locator('[data-slot=plan-error]')).toContainText('no longer waiting for a choice')
  // The options stay for another try.
  await expect(dialog.getByRole('radio')).toHaveCount(3)
  expect(api.applies).toEqual([])
  await shot(page, 'repair-plan-refused-768', false)
  await expectClean(w)
})

test('a reply in your own words is previewed, sent with resume --reply and read back', async ({ page }) => {
  const { w, api } = await open(page, 1280, 'light')
  const { dialog } = await openRepair(page, 'acme/app', 108)
  const reply = dialog.getByRole('textbox', { name: 'Reply in your own words' })
  await reply.fill('  Migrate only the attachments of the last 90 days.  ')
  await dialog.getByRole('button', { name: 'Preview reply' }).click()
  await expect(dialog).toHaveAttribute('data-phase', 'preview')
  // The request carries the trimmed reply and no option.
  expect(api.plans).toEqual([{ issues: [choose], reply: 'Migrate only the attachments of the last 90 days.' }])
  await dialog.getByRole('button', { name: 'Send reply' }).click()
  await expect(dialog.locator('[data-summary]')).toHaveAttribute('data-summary', 'confirmed')
  expect(api.applies).toEqual([{ id: 'PLANREPAIR', body: { confirm: true } }])
  await expect(dialog.locator(`[data-item="decision:${choose}"] [data-slot=check]`)).toContainText('a reply by octo-dev')
  await shot(page, 'repair-reply-confirmed-light-1280', false)
  await dialog.getByRole('button', { name: 'Close' }).first().click()
  await expect(page.locator('[data-rereads="1"]')).toHaveCount(1)
  await expectClean(w)
})

test('emptying the reply brings back the last picked option', async ({ page }) => {
  const { w, api } = await open(page, 375)
  const { dialog } = await openRepair(page, 'acme/app', 108)
  await dialog.getByRole('radio', { name: /Stop and split the Issue/ }).check()
  const reply = dialog.getByRole('textbox', { name: 'Reply in your own words' })
  await reply.fill('Split it.')
  await expect(dialog.getByRole('radio', { checked: true })).toHaveCount(0)
  await reply.fill('   ')
  await expect(dialog.getByRole('radio', { name: /Stop and split the Issue/ })).toBeChecked()
  await expect(dialog.getByRole('button', { name: 'Submit' })).toBeEnabled()
  // Picking a card after typing clears the reply.
  await reply.fill('Split it.')
  await dialog.getByRole('radio', { name: /Move new uploads only/ }).check()
  await expect(reply).toHaveValue('')
  await dialog.getByRole('button', { name: 'Submit' }).click()
  await expect(dialog.locator('[data-summary]')).toHaveAttribute('data-summary', 'confirmed')
  expect(api.plans).toEqual([{ issues: [choose], option: 1 }])
  await expectClean(w)
})
