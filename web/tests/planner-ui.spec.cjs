// Optional browser regressions. Build with make build, then run with a local Playwright installation:
// NODE_PATH=/path/to/node_modules /path/to/node_modules/.bin/playwright test web/tests/planner-ui.spec.cjs
// DOCKET_TEST_BINARY may point to another freshly built Docket binary.
const { test: base, expect } = require('playwright/test')
const { spawn } = require('node:child_process')
const { once } = require('node:events')
const { mkdtemp, rm } = require('node:fs/promises')
const { createServer } = require('node:net')
const { tmpdir } = require('node:os')
const path = require('node:path')

const longTitle = 'Review ' + 'ReleaseCandidate'.repeat(5)
const test = base.extend({
  planner: async ({ playwright }, use) => {
    const dir = await mkdtemp(path.join(tmpdir(), 'docket-browser-'))
    const socket = createServer()
    socket.listen(0, '127.0.0.1')
    await once(socket, 'listening')
    const addr = '127.0.0.1:' + socket.address().port
    await new Promise(resolve => socket.close(resolve))
    const binary = process.env.DOCKET_TEST_BINARY || path.resolve(__dirname, '../../bin/docket')
    const server = spawn(binary, [], {
      env: { PATH: process.env.PATH, DOCKET_MODE: 'local', DOCKET_ADDR: addr, DOCKET_DB: path.join(dir, 'ui.db'), DOCKET_BACKUP_DIR: path.join(dir, 'backups') },
      stdio: 'ignore',
    })
    const stopped = once(server, 'exit')
    const url = 'http://' + addr
    const api = await playwright.request.newContext({ baseURL: url, failOnStatusCode: true })
    try {
      await expect.poll(async () => {
        try { return (await api.get('/healthz')).ok() } catch { return false }
      }).toBe(true)
      await api.put('/api/profile', { data: { name: 'Lee' } })
      const project = await (await api.post('/api/projects', { data: { name: 'Release', key: 'REL' } })).json()
      const create = async (title, status, planned_for) => (await api.post('/api/tasks', { data: { project: 'REL', title, status, planned_for } })).json()
      const one = await create('Review text', 'backlog', '2001-01-01')
      const next = await create('Review links', 'todo')
      const long = await create(longTitle, 'done')
      await use({ url, board: '/p/' + project.slug, one, next, long, api })
    } finally {
      await api.dispose()
      server.kill()
      await stopped
      await rm(dir, { recursive: true, force: true })
    }
  },
})
test.use({ serviceWorkers: 'block', locale: 'en-GB', viewport: { width: 390, height: 844 }, hasTouch: true })

test('swiping the card pans the board and tapping it opens the task', async ({ page, browserName, planner }) => {
  test.skip(browserName !== 'chromium', 'Continuous touch input uses Chromium CDP')
  await page.goto(planner.url + planner.board)
  const card = page.getByRole('link', { name: /Review text/ })
  await expect(card).toBeVisible()
  const board = page.locator('section').filter({ has: card })
  const box = await card.boundingBox()
  const touch = await page.context().newCDPSession(page)
  const x = box.x + box.width - 60, y = box.y + box.height / 2
  const requests = []
  page.on('request', r => { if (r.url().endsWith('/move')) requests.push(r.url()) })
  await touch.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] })
  for (let delta = 20; delta <= 160; delta += 20) {
    await touch.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x - delta, y }] })
  }
  await touch.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  await expect.poll(() => board.evaluate(el => el.scrollLeft)).toBeGreaterThan(30)
  expect(requests).toEqual([])
  await card.tap()
  await expect(page.getByRole('dialog', { name: 'Task ' + planner.one.code })).toBeVisible()
})

test.describe('board input', () => {
  test.use({ viewport: { width: 1600, height: 900 } })

  async function expectSavedStatus(page, planner, status, statusName) {
    await expect.poll(async () => {
      const response = await planner.api.get('/api/tasks/' + planner.one.code)
      return (await response.json()).status
    }).toBe(status)
    await page.reload()
    await page.getByRole('button', { name: 'List', exact: true }).click()
    const row = page.getByRole('row').filter({ has: page.getByRole('cell', { name: planner.one.code, exact: true }) })
    await expect(row.getByRole('cell', { name: statusName, exact: true })).toBeVisible()
  }

  test('drag handle moves a task with the keyboard', async ({ page, planner }) => {
    await page.goto(planner.url + planner.board)
    await expect(page.getByRole('link', { name: /Review links/ })).toBeVisible()
    await page.evaluate(() => document.fonts.ready)
    await page.getByRole('button', { name: 'Drag task ' + planner.one.code }).press('Space')
    const announcement = page.getByRole('status')
    await expect(announcement).toContainText('over droppable area ' + planner.one.id + '.')
    // The keyboard sensor attaches after activation and measures column positions after paint.
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    await page.keyboard.press('ArrowRight')
    await expect(announcement).toContainText(new RegExp('over droppable area (' + planner.next.id + '|col:todo)\\.'))
    await page.keyboard.press('Space')
    await expectSavedStatus(page, planner, 'todo', planner.next.status_name)
  })

  test('drag handle moves a task into an empty column with the mouse', async ({ page, planner }) => {
    // Start with an empty backlog in this test's own database.
    await planner.api.post('/api/tasks/' + planner.one.code + '/move', { data: { status: 'todo' } })
    await page.goto(planner.url + planner.board)
    const handle = page.getByRole('button', { name: 'Drag task ' + planner.one.code })
    const backlog = page.getByRole('button', { name: 'Add a task to “' + planner.one.status_name + '”' })
    await handle.dragTo(backlog, { steps: 16 })
    await expect(page.getByRole('status')).toContainText('dropped over droppable area col:backlog')
    await expectSavedStatus(page, planner, 'backlog', planner.one.status_name)
  })

  test('Enter opens the task from its board link', async ({ page, planner }) => {
    await page.goto(planner.url + planner.board)
    await page.getByRole('link', { name: /Review text/ }).press('Enter')
    await expect(page.getByRole('dialog', { name: 'Task ' + planner.one.code })).toBeVisible()
  })
})

test('mobile menu keeps focus visible and closes on repeated or intra-page navigation', async ({ page, browserName, planner }) => {
  await page.goto(planner.url)
  const toggle = page.locator('button[aria-controls="app-sidebar"]')
  await expect(toggle).toBeVisible()
  // WebKit on macOS uses Option+Tab to include buttons in keyboard navigation.
  await page.keyboard.press(browserName === 'webkit' && process.platform === 'darwin' ? 'Alt+Tab' : 'Tab')
  await expect(toggle).toBeFocused()
  await page.keyboard.press('Enter')
  const menu = page.getByRole('dialog')
  await expect(menu).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(toggle).toBeFocused()
  for (let i = 0; i < 2; i++) {
    await toggle.click()
    const overdue = menu.locator('a[href="/?focus=overdue"]')
    if (i) await expect(overdue).toHaveAttribute('aria-current', 'page')
    await overdue.click()
    await expect(menu).toHaveCount(0)
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await expect(page).toHaveURL(/focus=overdue/)
  }
  await page.setViewportSize({ width: 1440, height: 900 })
  await expect(page.locator('#app-sidebar a[href="/?focus=overdue"]')).toBeVisible()
})

test('global search respects the active dialog', async ({ page, planner }) => {
  await page.goto(planner.url)
  await page.getByRole('button', { name: 'Profile and settings' }).click()
  const dialog = page.getByRole('dialog')
  const input = dialog.getByRole('textbox').first()
  await input.focus()
  for (const shortcut of ['Control+k', 'Meta+k']) {
    await page.keyboard.press(shortcut)
    await expect(dialog).toHaveCount(1)
    await expect(input).toBeFocused()
  }
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await page.keyboard.press('Control+k')
  await expect(page.getByRole('dialog', { name: 'Search' })).toBeVisible()
})

test('long technical titles wrap in the page, drawer and board', async ({ page, planner }) => {
  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 844 })
    for (const route of ['/t/' + planner.long.code, planner.board + '?task=' + planner.long.code, planner.board]) {
      await page.goto(planner.url + route)
      const title = route === planner.board ? page.getByText(longTitle, { exact: true }) : page.getByRole('heading', { name: longTitle })
      await expect(title).toBeVisible()
      await expect.poll(() => title.evaluate(el => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(1)
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
    }
  }
})

test('project filters show matches, explain empty results and restore the clicked list row', async ({ page, planner }) => {
  await page.setViewportSize({ width: 1120, height: 900 })
  await page.goto(planner.url + planner.board)
  const search = page.getByRole('textbox', { name: 'Filter by title' })
  for (const layout of ['Board', 'List']) {
    await page.getByRole('button', { name: layout, exact: true }).click()
    await search.fill('Review links')
    await expect(page.getByText(/showing 1 of 3/)).toBeVisible()
    await search.fill('unmatched title')
    await expect(page.getByText('No tasks match these filters')).toBeVisible()
    await page.getByRole('button', { name: 'Clear filters' }).click()
    await expect(search).toHaveValue('')
    await expect(page.getByRole('link', { name: /Review links/ })).toBeVisible()
  }
  const row = page.getByRole('row').filter({ hasText: planner.next.code })
  const status = await row.getByRole('cell').nth(2).boundingBox()
  await page.mouse.click(status.x + status.width / 2, status.y + status.height / 2)
  await expect(page.getByRole('dialog', { name: 'Task ' + planner.next.code })).toBeVisible()
})

test('mobile view options fit and the day menu confirms a carry before saving', async ({ page, planner }) => {
  const day = '2001-01-01'
  await page.setViewportSize({ width: 320, height: 700 })
  await page.goto(planner.url + planner.board)
  const options = page.getByRole('button', { name: 'View options' })
  await options.click()
  for (const trigger of await page.locator('#board-options button[aria-haspopup="menu"]').all()) {
    await trigger.click()
    const menu = page.getByRole('menu')
    const box = await menu.boundingBox()
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(320)
    await page.keyboard.press('Escape')
  }
  const savedDay = async () => (await (await planner.api.get('/api/tasks/' + planner.one.code)).json()).planned_for
  await page.goto(planner.url + '/?date=' + day)
  const status = page.getByRole('button', { name: new RegExp(planner.one.code + '.*Change status') })
  const target = await status.boundingBox()
  expect(Math.min(target.width, target.height)).toBeGreaterThanOrEqual(32)
  await expect(page.getByRole('button', { name: 'Next day' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'New task', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Day actions' }).click()
  const carry = page.getByRole('menu').getByRole('button')
  await carry.click()
  await expect(carry).toHaveAccessibleName(/Move 1 to/)
  expect(await savedDay()).toBe(day)
  await carry.click()
  await expect.poll(savedDay).toBe('2001-01-02')
  await expect(page.getByRole('menu')).toHaveCount(0)
})

test('MCP reports the server check and the last logged call separately', async ({ page, planner }) => {
  const state = { online: false, calls_today: 0, median_ms: 0, last_call_at: '', token_last_used_at: '', check: null }
  const stamp = '2002-06-11T10:00:00Z'
  await page.route('**/api/mcp/status', route => route.fulfill({ json: state }))
  for (const [check, online, last, headline] of [
    [null, false, '', 'MCP has not been checked yet'],
    [{ at: stamp, ok: false, checks: [] }, false, '', 'MCP self-check failed'],
    [{ at: stamp, ok: true, checks: [] }, true, '', 'MCP is ready to connect'],
    [{ at: stamp, ok: true, checks: [] }, true, stamp, 'MCP server is available'],
  ]) {
    Object.assign(state, { online, check, last_call_at: last })
    for (const screen of ['/', '/integrations']) {
      await page.goto(planner.url + screen)
      await expect(page.getByText(headline, { exact: true })).toBeVisible()
      await expect(page.getByText(last ? /Last call · .*Jun/ : 'No calls in the log yet').first()).toBeVisible()
    }
  }
})

test('keyboard focus contrasts with the surface and navigation respects reduced motion', async ({ page, planner }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.addInitScript(() => {
    const scroll = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = function (options) {
      if (options?.block === 'start') document.documentElement.dataset.sectionScroll = options.behavior
      return scroll.call(this, options)
    }
  })
  for (const theme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' })
    await page.goto(planner.url + '/?focus=overdue')
    await expect(page.locator('html')).toHaveAttribute('data-section-scroll', 'instant')
    const link = page.locator('#app-sidebar a[aria-current="page"]')
    await page.keyboard.press('Tab')
    await link.focus()
    const contrasts = await link.evaluate(el => {
      const style = getComputedStyle(el)
      const ctx = document.createElement('canvas').getContext('2d')
      function luminance(color, surface) {
        ctx.fillStyle = surface
        ctx.fillRect(0, 0, 1, 1)
        ctx.fillStyle = color
        ctx.fillRect(0, 0, 1, 1)
        const [r, g, b] = [...ctx.getImageData(0, 0, 1, 1).data].map(v => {
          const c = v / 255
          return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
        })
        return 0.2126 * r + 0.7152 * g + 0.0722 * b
      }
      return [getComputedStyle(document.body).backgroundColor, style.backgroundColor].map(surface => {
        const a = luminance(style.outlineColor, surface), b = luminance(surface, surface)
        return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
      })
    })
    await expect(link).toHaveCSS('outline-style', 'solid')
    expect(Math.min(...contrasts)).toBeGreaterThanOrEqual(3)
    await expect(page.locator('#app-sidebar')).toHaveCSS('transition-duration', '0s')
  }
})

test('onboarding and activity use localized names and separate the author', async ({ page, planner }) => {
  for (const [lang, heading, created] of [
    ['ru', 'Как вас называть?', 'Создана задача'],
    ['en', 'What should we call you?', 'Task ' + planner.next.code + ' created'],
    ['uk', 'Як до вас звертатися?', 'Створено завдання'],
  ]) {
    await page.goto(planner.url)
    await page.evaluate(lang => localStorage.setItem('docket-lang', lang), lang)
    await page.route('**/api/profile', route => route.fulfill({ json: { name: '' } }))
    await page.reload()
    await expect(page.getByRole('heading', { name: heading })).toBeVisible()
    await page.unroute('**/api/profile')
    await page.reload()
    const event = page.getByText(created, { exact: lang === 'en' }).first()
    await expect(event).toBeVisible()
    await expect(event.locator('..').locator('time')).toBeVisible()
    await expect(event.locator('..')).toContainText('Lee')
  }
})

test('an empty project and a capped completed column show the right counts', async ({ page, planner }) => {
  const project = await (await planner.api.post('/api/projects', { data: { name: 'Archive', key: 'ARC' } })).json()
  const url = planner.url + '/p/' + project.slug
  await page.goto(url)
  await expect(page.getByText('No tasks in this project yet')).toBeVisible()
  for (let i = 1; i <= 22; i++) {
    await planner.api.post('/api/tasks', { data: { project: 'ARC', title: 'Archive ' + i, status: 'done' } })
  }
  await page.reload()
  await expect(page.getByText(/showing 20 of 22/)).toBeVisible()
  const expand = page.getByRole('button', { name: 'Show all 22' })
  await expect(expand).toBeVisible()
  const search = page.getByRole('textbox', { name: 'Filter by title' })
  await search.fill('Archive 2')
  await expect(page.getByText('4 of 22', { exact: true })).toBeVisible()
  await expect(expand).toHaveCount(0)
  await search.fill('')
  await expand.click()
  await expect(page.getByText(/showing 22 of 22/)).toBeVisible()
  await page.reload()
  await page.getByRole('button', { name: 'List', exact: true }).click()
  await expect(page.locator('tbody tr')).toHaveCount(22)
})
