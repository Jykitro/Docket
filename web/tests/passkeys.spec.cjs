// Browser passkey regression with an isolated server and Chromium's virtual authenticator.
const { test, expect } = require('playwright/test')
const { spawn } = require('node:child_process')
const { once } = require('node:events')
const { mkdtemp, rm } = require('node:fs/promises')
const { createServer } = require('node:net')
const { tmpdir } = require('node:os')
const path = require('node:path')

test.use({ locale: 'en-GB', viewport: { width: 1280, height: 900 }, serviceWorkers: 'block' })

test('enroll a passkey, sign into its workspace and revoke it', async ({ page, browserName }) => {
  test.skip(browserName !== 'chromium', 'This test uses the Chromium virtual authenticator')
  const dir = await mkdtemp(path.join(tmpdir(), 'plandillo-passkeys-'))
  const socket = createServer()
  socket.listen(0, '127.0.0.1')
  await once(socket, 'listening')
  const port = socket.address().port
  await new Promise(resolve => socket.close(resolve))
  const url = 'http://localhost:' + port
  const binary = process.env.PLANDILLO_TEST_BINARY || path.resolve(__dirname, '../../bin/plandillo')
  const app = spawn(binary, [], { env: { PATH: process.env.PATH, PLANDILLO_MODE: 'selfhost',
    PLANDILLO_ADDR: '127.0.0.1:' + port, PLANDILLO_DB: path.join(dir, 'planner.db'),
    PLANDILLO_BACKUP_DIR: path.join(dir, 'backups') }, stdio: ['ignore', 'ignore', 'pipe'] })
  const stopped = once(app, 'exit')
  try {
    await expect.poll(() => fetch(url + '/healthz').then(r => r.ok, () => false)).toBe(true)
    const password = 'test-passkey-password'
    const post = (route, data) => page.request.post(url + route, { data, failOnStatusCode: true })
    await post('/api/auth/setup', { username: 'owner', password })
    await post('/api/projects', { name: 'Owner workspace', key: 'OWN' })
    const cdp = await page.context().newCDPSession(page)
    await cdp.send('WebAuthn.enable')
    await cdp.send('WebAuthn.addVirtualAuthenticator', { options: {
      protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true,
    } })
    await page.goto(url)
    const settings = page.getByRole('button', { name: 'Settings', exact: true })
    await settings.click()
    const keys = page.getByRole('region', { name: 'Passkeys' })
    await keys.getByRole('button', { name: 'Add a passkey' }).click()
    await keys.getByLabel('Passkey name').fill('Laptop')
    await keys.getByLabel('Current password').fill(password)
    await keys.getByRole('button', { name: 'Confirm' }).click()
    const revoke = keys.getByRole('button', { name: 'Revoke passkey Laptop' })
    await expect(revoke).toBeVisible()
    const logout = page.getByRole('button', { name: 'Sign out', exact: true })
    await logout.click()
    const login = page.getByRole('button', { name: 'Sign in with a passkey' })
    await login.click()
    await expect(page.getByRole('link', { name: 'Owner workspace' }).first()).toBeVisible()
    const me = await (await page.request.get(url + '/api/auth/me')).json()
    expect(me.user.username).toBe('owner')
    await settings.click()
    await revoke.click()
    await keys.getByLabel('Current password').fill(password)
    await keys.getByRole('button', { name: 'Confirm' }).click()
    await expect(revoke).toHaveCount(0)
    await logout.click()
    await login.click()
    await expect(page.getByRole('alert')).toContainText('passkey verification failed')
    expect((await page.request.get(url + '/api/auth/me')).status()).toBe(401)
    await page.locator('input[autocomplete=username]').fill('owner')
    await page.locator('input[autocomplete=current-password]').fill(password)
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await expect(page.getByRole('link', { name: 'Owner workspace' }).first()).toBeVisible()
  } finally {
    app.kill()
    await stopped
    await rm(dir, { recursive: true, force: true })
  }
})
