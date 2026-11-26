import { workspaceApi } from './workspacePath'
// REST client, types and shared data hooks. Every list is a TanStack Query; SSE (live.ts) invalidates them.
import { onlineManager, useQueries, useQuery } from '@tanstack/react-query'
import { getLang, locale, tr } from './i18n'
import { CollectionBudget } from './Paging'
import { wipeOfflineCache } from './offlineCache'

export type Category = 'todo' | 'doing' | 'done'

export type Project = {
  id: number; slug: string; key: string; name: string; icon: string; icon_url?: string; color: string; description: string
  position: number; archived_at: string | null; deleted_at: string | null; counts: Record<string, number>; open: number
}
export type Status = {
  id: number; project_id: number; key: string; name: string; color: string; category: Category
  show_on_board: boolean; position: number; task_count: number
}
export type Label = { id: number; project_id: number; name: string; color: string; task_count: number }
export type Field = {
  id: number; project_id: number; name: string; type: 'number' | 'url' | 'select'; options: string[]
  show_on_card: boolean; position: number
}
export type Task = {
  issue_kind?: string; report_count?: number; attention_after?: string
  id: number; project_id: number; number: number; code: string; parent_id: number | null; parent_code: string
  title: string; description_md: string; has_description?: boolean; status_id: number; status: string; status_name: string
  status_category: Category; priority: 'P0' | 'P1' | 'P2'; area: string; planned_for: string | null
  position: number; custom: Record<string, string | number>; created_by: string
  created_at: string; updated_at: string; done_at: string | null; deleted_at: string | null; subtask_count: number; subtask_done: number
}
type TrashVersion = { id: number; deleted_at: string }
export type Activity = {
  id: number; task_id: number | null; project_id: number | null; actor: string; action: string
  diff_json: string; at: string; task_code: string; task_title: string
}
export type TodayData = { date: string; projects: Project[]; tasks: Task[]; overdue: Task[]; activity: Activity[] }
export type Profile = { name: string }
type Check = { key: string; label: string; ok: boolean; detail: string; value: string; hint?: string }
export type CheckResult = { ok: boolean; took_ms: number; at: string; checks: Check[] }
export type McpStatus = {
  online: boolean; calls_today: number; median_ms: number; last_call_at: string; token_last_used_at: string; check: CheckResult | null
}
export type McpTool = { name: string; group: 'read' | 'write'; description: string; enabled: boolean; calls_7d: number }
export type McpSettings = { access_mode: 'rw' | 'ro'; tools: McpTool[]; token_masked: string; token_from_env: boolean }
export type TokenInfo = { token: string; masked: string; from_env: boolean }
export type McpCall = {
  id: number; at: string; tool: string; args_summary: string; duration_ms: number; status: 'ok' | 'error'; error_code: string | null; actor: string
}
export type ViewFilter = {
  q?: string; priority?: string[]; area?: string[]; date?: string[]; author?: string[]; fields?: Record<string, string[]>
}
export type View = {
  id: number; project_id: number | null; name: string; layout: 'board' | 'list'; filter: ViewFilter
  group_by: string; sort: string; pinned: boolean; position: number
}
export type NewTask = {
  project: string; title: string; description_md?: string; status?: string; priority?: string; area?: string; planned_for?: string; parent?: string
}
export type Note = { id: number; task_id: number; author: string; body_md: string; created_at: string }
export type AttentionItem = Pick<Task, 'id' | 'code' | 'title' | 'status_name' | 'status_category' | 'priority' | 'issue_kind' | 'report_count'> & { project: string }
export type HTMLDocumentInfo = { id: number; title: string; version: number; author: string; created_at: string }
export type TaskDetail = Task & { documents?: HTMLDocumentInfo[]; subtasks: Task[]; notes: Note[]; activity: Activity[] }
export type Hit = {
  id: number; project_id: number; code: string; title: string; status: string; status_name: string; status_color: string; snippet?: string
}
export type SearchResult = {
  groups: { tasks: Hit[]; descriptions: Hit[]; projects: Project[] }; counts: Record<string, number>; took_ms: number
}

export type Page = {
  id: number; project_id: number; parent_id: number | null; title: string; body_md: string; body_html?: string; position: number; pinned: boolean
  created_at: string; updated_at: string
}
export type PageRef = { id: number; title: string }
export type TaskRef = { code: string; title: string }
export type TaskPage = PageRef & { project: string }
export type PageDetail = Page & { documents?: HTMLDocumentInfo[]; backlinks: PageRef[]; links: { pages: PageRef[]; tasks: TaskRef[] } }

export type AuthUser = { name: string; role: string; is_active: boolean; photo: string | null; updated_at: string; id: number; username: string; is_admin: boolean; blocked?: boolean; created_at: string }
export type Registration = 'open' | 'invite' | 'closed'
type Recovery = { recovery_code: string }
export type Me = { mode: 'local' | 'selfhost'; setup?: boolean; user?: AuthUser }
type BackupFile = { name: string; size: number; created_at: string }
type Backups = { available: boolean; keep?: number; backups: BackupFile[] }
export type AdminInvite = { id: number; created_at: string; expires_at: string }
export type NewInvite = { url: string; expires_at: string }

export class ApiError extends Error {
  constructor(public code: string, message: string, public status = 0) { super(message) }
}

/** What kind of failure an error is, so the UI can explain it in plain words instead of showing a raw server text. */
export type ErrorKind = 'offline' | 'auth' | 'forbidden' | 'not_found' | 'rate' | 'server' | 'other'
export function errorKind(e: unknown): ErrorKind {
  if (e instanceof TypeError) return 'offline' // fetch rejects with a TypeError when the server cannot be reached
  if (!(e instanceof ApiError)) return 'other'
  if (e.status === 401 || e.code === 'unauthorized') return 'auth'
  if (e.status === 403 || e.code === 'forbidden') return 'forbidden'
  if (e.status === 404 || e.code === 'not_found') return 'not_found'
  if (e.status === 429) return 'rate'
  if (e.status >= 502 && e.status <= 504) return 'offline' // the proxy in front of Plandillo has no server behind it
  if (e.status >= 500) return 'server'
  return 'other'
}

// Server texts are English. Each known one maps to [ru, uk]; English and unknown ones show the server's own message.
// Keys are the exact messages from internal/selfhost/handler.go and internal/auth/db.go: update both together.
const SERVER: Record<string, [string, string]> = {
  'passkey verification failed; try again': ['Не удалось подтвердить ключ доступа. Попробуйте ещё раз.', 'Не вдалося підтвердити ключ доступу. Спробуйте ще раз.'],
  'passkey already registered': ['Этот ключ уже добавлен.', 'Цей ключ уже додано.'],
  'passkey limit reached; revoke an unused key': ['Достигнут предел в 32 ключа. Отзовите неиспользуемый ключ.', 'Досягнуто межі у 32 ключі. Відкличте невикористовуваний ключ.'],
  'passkey not found': ['Ключ доступа не найден', 'Ключ доступу не знайдено'],
  'too many pending passkey requests': ['Слишком много запросов. Попробуйте позже.', 'Забагато запитів. Спробуйте пізніше.'],
  'passkey name must be 1-64 characters': ['Название ключа должно содержать от 1 до 64 символов.', 'Назва ключа має містити від 1 до 64 символів.'],
  'invalid username or password': ['Неверное имя пользователя или пароль', 'Неправильне ім’я користувача або пароль'],
  'current password is wrong': ['Неверный текущий пароль', 'Неправильний поточний пароль'],
  'too many failed attempts, try again later': ['Слишком много попыток, подождите несколько минут.', 'Забагато спроб, зачекайте кілька хвилин.'],
  'username is taken': ['Имя пользователя уже занято', 'Ім’я користувача вже зайняте'],
  'invalid or expired invite': ['Приглашение недействительно или истекло', 'Запрошення недійсне або втратило чинність'],
  'username must be 2-32 characters: a-z, 0-9, _ . -, starting with a letter or digit': [
    'Имя пользователя: 2-32 символа, a-z, 0-9, _ . -, начинается с буквы или цифры',
    'Ім’я користувача: 2-32 символи, a-z, 0-9, _ . -, починається з літери або цифри',
  ],
  'keep at least one active workspace': ['Оставьте хотя бы одно активное пространство.', 'Залиште хоча б один активний простір.'],
  'invalid workspace name, icon or action': ['Проверьте название, значок и действие с пространством.', 'Перевірте назву, піктограму та дію з простором.'],
  'workspace not found': ['Пространство недоступно.', 'Простір недоступний.'],
  'password must be 10-128 characters': ['Пароль должен быть от 10 до 128 символов', 'Пароль має бути від 10 до 128 символів'],
  'cannot delete yourself': ['Нельзя удалить самого себя', 'Не можна видалити самого себе'],
  'not logged in': ['Сессия истекла, войдите снова', 'Сесія завершилася, увійдіть знову'],
  'registration is unavailable': ['Регистрация недоступна', 'Реєстрація недоступна'],
  'workspace limit reached': ['Достигнут предел рабочих пространств', 'Досягнуто ліміту робочих просторів'],
  'invalid credentials': ['Неверные учётные данные', 'Неправильні облікові дані'],
  'isolated Code Mode runner is unavailable': ['Исполнитель Code Mode недоступен', 'Виконавець Code Mode недоступний'],
  'the last administrator must remain active': ['Последний администратор должен оставаться активным', 'Останній адміністратор має залишатися активним'],
  'too many requests, try again later': ['Слишком много запросов, попробуйте позже', 'Забагато запитів, спробуйте пізніше'],
  'admin only': ['Только для администратора', 'Лише для адміністратора'],
  'trash changed; reload and review before purging': ['Состав корзины изменился. Проверьте список и подтвердите очистку ещё раз.', 'Вміст кошика змінився. Перегляньте список і підтвердьте очищення ще раз.'],
}
export const errText = (e: unknown): string => {
  if (!(e instanceof Error)) return String(e)
  const kind = errorKind(e)
  if (kind === 'offline') return tr('Нет связи с сервером. Проверьте подключение и повторите.')
  // A known server text (e.g. the Code Mode runner) says more than a generic line; proxies' "Bad Gateway" does not.
  if (kind === 'server' && !(e.message in SERVER)) return tr('Сбой на сервере. Повторите через минуту.')
  const lang = getLang()
  return (lang !== 'en' && SERVER[e.message]?.[lang === 'ru' ? 0 : 1]) || e.message
}

const sessionRequests = new AbortController()
const changeKey = 'docket-auth-change'
let owner: number | 'guest' | 'setup' | 'local' | undefined
function sessionStamp() {
  try { return localStorage.getItem(changeKey) } catch { return null }
}
let stamp = sessionStamp()
// This document's own sign-in (kept current by an in-document sign-in), not the live one: a tab resumed after another
// tab signed in must not write its data under the new stamp before it reloads.
export function offlineStamp() { return stamp ?? 'local' }
let sessionGeneration = 0

// The last workspace list, tagged with the sign-in stamp: switching workspaces reloads the page, and this lets the
// switcher draw the right name and icon at once instead of a placeholder until /api/workspaces answers.
const spacesKey = 'plandillo-spaces'
export function cachedSpaces(): Space[] | undefined {
  try {
    const c = JSON.parse(localStorage.getItem(spacesKey) ?? 'null') as { stamp: string | null; rows: Space[] } | null
    return c && c.stamp === sessionStamp() ? c.rows : undefined
  } catch { return undefined }
}
export function cacheSpaces(rows: Space[]) {
  try { localStorage.setItem(spacesKey, JSON.stringify({ stamp: sessionStamp(), rows })) } catch { /* storage can be blocked: no warm start */ }
}

// A document owns exactly one private workspace. Reloading discards React state, queued
// mutations, credentials and the query cache together, even if two profiles reuse row IDs.
function leaveWorkspace() {
  if (!sessionRequests.signal.aborted) {
    sessionRequests.abort()
    if ('clearAppBadge' in navigator) void navigator.clearAppBadge().catch(() => {}) // the signed-out app icon keeps no count
    // The reload must happen even if the wipe fails or hangs.
    void Promise.race([wipeOfflineCache(), new Promise(r => setTimeout(r, 1000))]).finally(() => location.reload())
  }
}
function checkSessionStamp() {
  if (stamp !== sessionStamp()) leaveWorkspace()
}
window.addEventListener('storage', event => {
  if (event.key === changeKey || event.key === null) checkSessionStamp()
})
window.addEventListener('pageshow', checkSessionStamp)
window.addEventListener('focus', checkSessionStamp)

function assertSessionCurrent() {
  checkSessionStamp()
  sessionRequests.signal.throwIfAborted()
}

const identityOf = (me: { user?: { id?: number }; mode?: string; setup?: boolean } | null | undefined) =>
  me?.user?.id ?? (me?.mode === 'local' ? 'local' : me?.setup ? 'setup' : 'guest')

// Data restored from the device belongs to whoever /me said last: the usual identity checks (401, changed user) then apply to it.
export function adoptRestoredMe(me: unknown) {
  if (owner === undefined && me && typeof me === 'object') owner = identityOf(me)
}

async function req<T>(method: string, url: string, body?: unknown, budget?: CollectionBudget): Promise<T> {
  assertSessionCurrent()
  const started = sessionGeneration
  const headers = new Headers()
  if (body !== undefined) headers.set('Content-Type', 'application/json')
  if (typeof owner === 'number' && url !== '/api/auth/me') headers.set('X-Docket-User', String(owner))
  const res = await fetch(workspaceApi(url), {
    method, headers,
    signal: sessionRequests.signal,
    body: body === undefined ? undefined : JSON.stringify(body),
  }).catch((error: unknown) => {
    if (!sessionRequests.signal.aborted) window.dispatchEvent(new Event('plandillo:disconnected'))
    throw error
  })
  if (res.status >= 502 && res.status <= 504) window.dispatchEvent(new Event('plandillo:disconnected'))
  let data
  if (res.status !== 204) {
    if (res.ok && budget) {
      const raw = await res.arrayBuffer()
      budget.consume(raw.byteLength)
      data = JSON.parse(new TextDecoder().decode(raw))
    } else data = await res.json().catch(() => null)
  }
  assertSessionCurrent()
  if (started !== sessionGeneration) throw new DOMException('Session changed', 'AbortError')
  if (url === '/api/auth/me' && (res.ok || res.status === 401)) {
    const observed = identityOf(data)
    if (owner !== undefined && observed !== owner) leaveWorkspace()
    sessionRequests.signal.throwIfAborted()
    owner = observed
  }
  if (data?.error?.code === 'session_changed' || (res.status === 401 && typeof owner === 'number' && (data?.error?.code === 'unauthorized' || !url.startsWith('/api/auth/')))) leaveWorkspace()
  sessionRequests.signal.throwIfAborted()
  if (!res.ok) throw new ApiError(data?.error?.code ?? 'internal', data?.error?.message ?? data?.error_description ?? res.statusText, res.status)
  if (!data?.challenge && (['/api/auth/login/totp','/api/auth/login', '/api/auth/passkeys/login/finish', '/api/auth/logout', '/api/auth/setup', '/api/auth/register', '/api/auth/account', '/api/auth/recover'].includes(url) || url.startsWith('/api/auth/invite/'))) {
    sessionGeneration++
    try {
      localStorage.setItem(changeKey, crypto.getRandomValues(new Uint32Array(4)).join('-'))
      localStorage.removeItem(spacesKey) // another account must not see these names, even for a moment
      stamp = sessionStamp()
    } catch { /* /me also checks the identity directly when storage is unavailable */ }
    if (url === '/api/auth/logout' || url === '/api/auth/account') leaveWorkspace()
    sessionRequests.signal.throwIfAborted()
    if (typeof data?.id === 'number') owner = data.id
  }
  return data as T
}

// Fetch bounded pages from one profile; the query cache keeps the complete board for local filters.
async function collectPages<T>(url: string, size: (page: T) => number, merge: (all: T, page: T) => void) {
  const budget = new CollectionBudget()
  let all: T | undefined
  let limit = 100
  let offset = 0
  while (true) {
    let page: T
    try {
      await budget.before(url, offset, sessionRequests.signal)
      page = await req<T>('GET', url + (url.includes('?') ? '&' : '?') + `limit=${limit}&offset=${offset}`, undefined, budget)
    } catch (err) {
      if (!(err instanceof ApiError && err.code === 'page_too_large' && limit > 1)) throw err
      limit = Math.max(1, Math.floor(limit / 2))
      continue
    }
    const count = size(page)
    if (all === undefined) all = page
    else merge(all, page)
    if (count < limit) return all
    offset += limit
  }
}

function listPages<T>(url: string) {
  return collectPages<T[]>(url, page => page.length, (all, page) => all.push(...page))
}

async function taskPages(ref: string) {
  const code = ref.trim().toUpperCase()
  if (!/^(?:[A-Z][A-Z0-9]{1,4}-)?[0-9]{1,19}$/.test(code)) throw new Error(tr('Некорректная ссылка на задачу'))
  return collectPages<TaskDetail>(`/api/tasks/${encodeURIComponent(code)}`, row => {
    if (!row || typeof row.code !== 'string' || typeof row.title !== 'string' || !Number.isSafeInteger(row.id)
      || !Array.isArray(row.notes) || !Array.isArray(row.activity) || !Array.isArray(row.subtasks)
      || row.documents != null && !Array.isArray(row.documents)) throw new Error(tr('Не удалось прочитать задачу'))
    return Math.max(row.subtasks.length, row.notes.length, row.documents?.length ?? 0)
  }, (all, page) => {
    all.subtasks.push(...page.subtasks)
    all.notes.push(...page.notes)
    if (page.documents?.length) (all.documents ??= []).push(...page.documents)
  })
}

function todayPages(date?: string) {
  return collectPages<TodayData>('/api/today' + (date ? `?date=${date}` : ''), page => Math.max(page.tasks.length, page.overdue.length), (all, page) => {
    all.tasks.push(...page.tasks)
    all.overdue.push(...page.overdue)
    const ids = new Set(all.projects.map(p => p.id))
    all.projects.push(...page.projects.filter(p => !ids.has(p.id)))
    all.projects.sort((a,b) => a.position - b.position || a.id - b.id)
  })
}

async function wikiPage(id: number) {
  const url = `/api/pages/${id}`
  const budget = new CollectionBudget()
  await budget.before(url, 0, sessionRequests.signal)
  const page = await req<PageDetail>('GET', url, undefined, budget)
  let count = Math.max(page.documents?.length ?? 0, page.backlinks.length)
  let offset = 100
  while (count === 100) {
    await budget.before(url, offset, sessionRequests.signal)
    const next = await req<PageDetail>('GET', url + `?document_offset=${offset}`, undefined, budget)
    if (next.documents?.length) (page.documents ??= []).push(...next.documents)
    page.backlinks.push(...next.backlinks)
    count = Math.max(next.documents?.length ?? 0, next.backlinks.length)
    offset += 100
  }
  return page
}

export type Space = { id: number; owner_user_id: number; name: string; slug: string; icon: string; has_icon: boolean; archived_at: number | null; updated_at: number }
type SpaceChange = { name?: string; icon?: string; icon_png?: string; action?: 'archive' | 'restore' }
type Device = { id: string; user_agent: string; current: boolean; created_at: string; last_seen_at: string; expires_at: string }
export const api = {
  oauthRequest: (query: string) => req<{client: string; redirect_uri: string; write: boolean}>('GET', '/api/auth/oauth/request' + query),
  oauthDecision: (query: string, workspace: number, allow: boolean, write: boolean) => req<{redirect: string}>('POST', '/api/auth/oauth/decision' + query, { workspace, allow, write }),
  oauthConnections: (workspace: string) => req<{id: string; name: string; scope: string}[]>('GET', '/api/auth/oauth/connections?workspace=' + workspace),
  revokeOAuth: (id: string) => req<void>('DELETE', '/api/auth/oauth/connections/' + encodeURIComponent(id)),
  spaces: () => req<Space[]>('GET', '/api/workspaces'),
  createSpace: (name: string) => req<Space>('POST', '/api/workspaces', {name}),
  changeSpace: (id: number, change: SpaceChange) => req<Space>('PATCH', `/api/workspaces/${id}`, change),
  deleteSpace: (id: number, name: string, password: string) => req<void>('DELETE', `/api/workspaces/${id}`, {name,password}),
  setPhoto: (photo: string | null) => req<void>('PUT','/api/auth/photo',{photo}),
  sessions: () => req<Device[]>('GET','/api/auth/sessions'),
  revokeSession: (id: string) => req<void>('DELETE','/api/auth/sessions/'+encodeURIComponent(id)),
  totp: () => req<{available:boolean; enabled:boolean}>('GET','/api/auth/totp'),
  beginTOTP: (password:string) => req<{secret:string;qr_png:string}>('POST','/api/auth/totp/begin',{password}),
  setTOTP: (enable:boolean,password:string,code:string) => req<void>('POST','/api/auth/totp/'+(enable?'confirm':'disable'),{password,code}),
  finishTOTP: (challenge:string,code:string) => req<AuthUser>('POST','/api/auth/login/totp',{challenge,code}),
  async me(): Promise<Me> {
    try {
      return await req<Me>('GET', '/api/auth/me')
    } catch (error) {
      if (error instanceof ApiError && error.code === 'unauthorized') return { mode: 'selfhost' }
      throw error
    }
  },
  login: (username: string, password: string) => req<AuthUser | {challenge:string}>('POST', '/api/auth/login', { username, password }),
  passkeys: () => req<{ available: boolean; keys: { id: string; name: string }[] }>('GET', '/api/auth/passkeys'),
  beginPasskeyLogin: () => req<{ publicKey: PublicKeyCredentialRequestOptionsJSON }>('POST', '/api/auth/passkeys/login/begin', {}),
  finishPasskeyLogin: (key: ReturnType<PublicKeyCredential['toJSON']>) => req<AuthUser>('POST', '/api/auth/passkeys/login/finish', key),
  beginPasskeyRegistration: (name: string, password: string) => req<{ publicKey: PublicKeyCredentialCreationOptionsJSON }>('POST', '/api/auth/passkeys/register/begin', { name, password }),
  finishPasskeyRegistration: (key: ReturnType<PublicKeyCredential['toJSON']>) => req<void>('POST', '/api/auth/passkeys/register/finish', key),
  revokePasskey: (id: string, password: string) => req<void>('DELETE', `/api/auth/passkeys/${encodeURIComponent(id)}`, { password }),
  // The server answers 204 to logout whether or not a session still exists.
  logout: () => req<void>('POST', '/api/auth/logout'),
  setup: (username: string, password: string) => req<AuthUser & Recovery>('POST', '/api/auth/setup', { username, password }),
  acceptInvite: (token: string, username: string, password: string) =>
    req<AuthUser & Recovery>('POST', `/api/auth/invite/${encodeURIComponent(token)}`, { username, password }),
  changePassword: (old: string, next: string) => req<void>('PUT', '/api/auth/password', { old, new: next }),
  adminUsers: () => req<AuthUser[]>('GET', '/api/admin/users'),
  adminBackups: () => req<Backups>('GET', '/api/admin/backups'),
  createBackup: () => req<BackupFile>('POST', '/api/admin/backups'),
  adminInvites: () => req<AdminInvite[]>('GET', '/api/admin/invites'),
  createInvite: () => req<NewInvite>('POST', '/api/admin/invites'),
  revokeInvite: (id: number) => req<void>('DELETE', `/api/admin/invites/${id}`),
  registration: () => req<{ mode: Registration; passkeys: boolean }>('GET', '/api/auth/registration'),
  setRegistration: (mode: Registration) => req<void>('PUT', '/api/admin/registration', { mode }),
  register: (username: string, password: string) => req<AuthUser & Recovery>('POST', '/api/auth/register', { username, password }),
  recover: (username: string, code: string, password: string) => req<Recovery>('POST', '/api/auth/recover', { username, code, password }),
  issueRecovery: (password: string) => req<Recovery>('POST', '/api/auth/recovery-code', { password }),
  blockUser: (id: number, blocked: boolean) => req<void>('PUT', `/api/admin/users/${id}/blocked`, { blocked }),
  deleteAccount: (password: string) => req<void>('DELETE', '/api/auth/account', { password }),
  profile: () => req<Profile>('GET', '/api/profile'),
  setProfile: (name: string) => req<Profile>('PUT', '/api/profile', { name }),
  projects: () => listPages<Project>('/api/projects?include_archived=1'),
  createProject: (b: { name: string; key?: string; icon?: string; icon_png?: string; color?: string }) => req<Project>('POST', '/api/projects', b),
  statuses: (pid: number) => req<Status[]>('GET', `/api/projects/${pid}/statuses`),
  labels: (pid: number) => req<Label[]>('GET', `/api/projects/${pid}/labels`),
  fields: (pid: number) => req<Field[]>('GET', `/api/projects/${pid}/fields`),
  readHTML: (id: number, version: number) => req<HTMLDocumentInfo & { html: string }>('GET', `/api/html-documents/${id}?version=${version}`),
  publishHTML: (data: { task?: string; page_id?: number; title: string; html: string; expected_version: number }) => req<HTMLDocumentInfo>('POST', '/api/html-documents', data),
  deleteHTML: (id: number, version: number) => req<void>('DELETE', `/api/html-documents/${id}?version=${version}`),
  attention: () => req<AttentionItem[]>('GET', '/api/attention'),
  triage: (id: number, action: string, value: string) => req<void>('POST', `/api/tasks/${id}/triage`, { action, until: action === 'snooze' ? value : undefined, reason: action === 'close' ? value : undefined }),
  today: todayPages,
  tasks: (project: string) => listPages<Task>(`/api/tasks?project=${encodeURIComponent(project)}&include_done=1&summary=1`),
  createTask: (b: NewTask) => req<Task>('POST', '/api/tasks', b),
  updateTask: (ref: string | number, b: Record<string, unknown>) => req<Task>('PATCH', `/api/tasks/${ref}`, b),
  moveTask: (ref: string | number, b: { status?: string; before_id?: number; after_id?: number }) =>
    req<Task>('POST', `/api/tasks/${ref}/move`, b),
  carryOver: (from: string, to: string) => req<{ moved: number }>('POST', '/api/tasks/carry-over', { from, to }),
  views: (project: number) => req<View[]>('GET', `/api/views?project=${project}`),
  createView: (b: Omit<View, 'id' | 'position' | 'pinned'> & { pinned?: boolean }) => req<View>('POST', '/api/views', b),
  mcpStatus: () => req<McpStatus>('GET', '/api/mcp/status'),
  mcpCheck: () => req<CheckResult>('POST', '/api/mcp/check'),
  mcpSettings: () => req<McpSettings>('GET', '/api/mcp/settings'),
  patchMcpSettings: (b: { access_mode?: string; tools?: Record<string, boolean> }) => req<McpSettings>('PATCH', '/api/mcp/settings', b),
  mcpToken: () => req<TokenInfo>('GET', '/api/mcp/token'),
  rotateMcpToken: () => req<TokenInfo>('POST', '/api/mcp/token/rotate'),
  mcpCalls: (filter: string, limit = 50) => req<McpCall[]>('GET', `/api/mcp/calls?filter=${filter}&limit=${limit}`),
  clearMcpCalls: () => req<void>('DELETE', '/api/mcp/calls'),
  updateProject: (id: number, b: Record<string, unknown>) => req<Project>('PATCH', `/api/projects/${id}`, b),
  archiveProject: (id: number, archived: boolean) => req<Project>('POST', `/api/projects/${id}/archive`, { archived }),
  deleteProject: (id: number) => req<Project>('DELETE', `/api/projects/${id}`),
  restoreProject: (id: number) => req<Project>('POST', `/api/projects/${id}/restore`),
  trashProjects: () => listPages<Project>('/api/trash/projects'),
  createStatus: (pid: number, b: { name: string; color?: string; category?: string }) => req<Status>('POST', `/api/projects/${pid}/statuses`, b),
  updateStatus: (id: number, b: Record<string, unknown>) => req<Status>('PATCH', `/api/statuses/${id}`, b),
  deleteStatus: (id: number, moveTo?: number) => req<void>('DELETE', `/api/statuses/${id}` + (moveTo ? `?move_to=${moveTo}` : '')),
  reorderStatuses: (pid: number, ids: number[]) => req<Status[]>('POST', `/api/projects/${pid}/statuses/reorder`, { ids }),
  createLabel: (pid: number, b: { name: string; color?: string }) => req<Label>('POST', `/api/projects/${pid}/labels`, b),
  updateLabel: (id: number, b: Record<string, unknown>) => req<Label>('PATCH', `/api/labels/${id}`, b),
  deleteLabel: (id: number) => req<void>('DELETE', `/api/labels/${id}`),
  createField: (pid: number, b: { name: string; type: string; options?: string[]; show_on_card?: boolean }) => req<Field>('POST', `/api/projects/${pid}/fields`, b),
  updateField: (id: number, b: Record<string, unknown>) => req<Field>('PATCH', `/api/fields/${id}`, b),
  deleteField: (id: number) => req<void>('DELETE', `/api/fields/${id}`),
  updateView: (id: number, b: Record<string, unknown>) => req<View>('PATCH', `/api/views/${id}`, b),
  deleteView: (id: number) => req<void>('DELETE', `/api/views/${id}`),
  trash: () => listPages<Task>('/api/trash'),
  purgeTrash: (snapshot: { tasks: TrashVersion[]; projects: TrashVersion[] }) => req<{ purged: number }>('DELETE', '/api/trash', snapshot),
  task: taskPages,
  deleteTask: (ref: string) => req<{ trashed: number }>('DELETE', `/api/tasks/${ref}`),
  restoreTask: (ref: string) => req<Task>('POST', `/api/tasks/${ref}/restore`),
  addNote: (ref: string, body_md: string) => req<Note>('POST', `/api/tasks/${ref}/notes`, { body_md }),
  taskPages: (ref: string) => req<TaskPage[]>('GET', `/api/tasks/${ref}/pages`),
  pages: (pid: number) => listPages<Page>(`/api/projects/${pid}/pages`),
  page: wikiPage,
  createPage: (pid: number, b: { title: string; body_md?: string; body_html?: string; parent_id?: number }) => req<Page>('POST', `/api/projects/${pid}/pages`, b),
  savePage: (id: number, b: { title?: string; body_md?: string; body_html?: string; parent_id?: number; expected_updated_at?: string }) => req<Page>('PUT', `/api/pages/${id}`, b),
  setPageLayout: (id: number, b: { pinned: boolean } | { before_id: number } | { after_id: number }) =>
    req<void>('PUT', `/api/pages/${id}/layout`, b),
  deletePage: (id: number) => req<void>('DELETE', `/api/pages/${id}`),
  search: (q: string, scope = 'all') => req<SearchResult>('GET', `/api/search?q=${encodeURIComponent(q)}&scope=${scope}&limit=8`),
}

// ---- hooks ----

export const useAttention = () => useQuery({ queryKey: ['attention'], queryFn: api.attention, refetchInterval: 60_000 })
// A lost connection pauses the refetch (the cached user stays) instead of failing it; auth and server errors still fail at once.
export const useMe = () => useQuery({ queryKey: ['me'], queryFn: api.me, retry: (n, e) => n < 1 && errorKind(e) === 'offline', staleTime: 0, refetchOnWindowFocus: 'always' })
export const useProjects = () => useQuery({ queryKey: ['projects'], queryFn: api.projects })
export const useProfile = () => useQuery({ queryKey: ['profile'], queryFn: api.profile })

export function isAgent(actor: string) {
  return !!actor && actor !== 'denis'
}

export function agentName(actor: string) {
  switch (actor) {
    case 'claude': return 'Claude'
    case 'codex': return 'Codex'
    case 'mcp': return tr('MCP-агент')
    default: return actor
  }
}

/** The profile owner's stored ID is independent of their display name. */
export function useWho() {
  const name = useProfile().data?.name || tr('Вы')
  return (actor: string) => (isAgent(actor) ? agentName(actor) : name)
}

export type Meta = { statuses: Status[]; labels: Label[]; fields: Field[] }

/** Statuses, labels and fields of several projects at once, keyed by project id. */
export function useProjectsMeta(ids: number[]): Record<number, Meta> {
  const qs = useQueries({
    queries: ids.flatMap((id) => [
      { queryKey: ['statuses', id], queryFn: () => api.statuses(id) },
      { queryKey: ['labels', id], queryFn: () => api.labels(id) },
      { queryKey: ['fields', id], queryFn: () => api.fields(id) },
    ]),
  })
  const out: Record<number, Meta> = {}
  ids.forEach((id, i) => {
    out[id] = {
      statuses: (qs[i * 3].data as Status[] | undefined) ?? [],
      labels: (qs[i * 3 + 1].data as Label[] | undefined) ?? [],
      fields: (qs[i * 3 + 2].data as Field[] | undefined) ?? [],
    }
  })
  return out
}

// ---- display helpers ----

export function mcpHealthLabel(status: McpStatus | undefined, failed: boolean) {
  if (failed) return tr('Не удалось получить статус MCP')
  if (!status) return onlineManager.isOnline() ? tr('Загрузка…') : tr('Нет связи')
  if (!status.check) return tr('MCP ещё не проверен')
  if (!status.online) return tr('Самопроверка MCP не пройдена')
  return status.last_call_at || status.token_last_used_at ? tr('Сервер MCP доступен') : tr('MCP готов к подключению')
}

export function mcpLastCallLabel(status: McpStatus) {
  return status.last_call_at ? tr('Последний вызов · {when}', { when: fmtStamp(status.last_call_at) }) : tr('В журнале пока нет вызовов')
}


/** Status colour: one of the four theme tokens or a custom hex. */
export const statusColor = (c: string) => (/^(bk|td|dg|dn)$/.test(c) ? `var(--${c})` : c)

/** Soft chip colours of a status (background and text), neutral for custom hex colours. */
export const statusChip = (c: string) =>
  /^(bk|td|dg|dn)$/.test(c) ? { background: `var(--${c}s)`, color: `var(--${c}t)` } : { background: 'var(--sk)', color: 'var(--t2)' }

const pad = (n: number) => String(n).padStart(2, '0')
export const isoDay = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
export const todayStr = () => isoDay(new Date())
export function addDays(day: string, n: number) {
  const d = new Date(day + 'T12:00:00')
  d.setDate(d.getDate() + n)
  return isoDay(d)
}

/** "Today", "Tomorrow" or "4 Nov" (in the current language). */
export function fmtDay(day: string) {
  const today = todayStr()
  if (day === today) return tr('Сегодня')
  if (day === addDays(today, 1)) return tr('Завтра')
  const [, m, d] = day.split('-').map(Number)
  return `${d} ${tr('янв фев мар апр мая июн июл авг сен окт ноя дек').split(' ')[m - 1]}`
}

/** A due date on cards and in lists: like fmtDay, or "через 3 дня" / "2 дня назад" with Appearance → Due dates → relative. */
export function fmtDue(day: string) {
  if (document.documentElement.dataset.dates !== 'relative') return fmtDay(day)
  const days = Math.round((Date.parse(day + 'T12:00:00') - Date.parse(todayStr() + 'T12:00:00')) / 86_400_000)
  const s = new Intl.RelativeTimeFormat(locale(), { numeric: 'auto' }).format(days, 'day')
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export function fmtLongDay(day: string) {
  const d = new Date(day + 'T12:00:00')
  const s = d.toLocaleDateString(locale(), { weekday: 'long', day: 'numeric', month: 'long' })
  return s.charAt(0).toUpperCase() + s.slice(1)
}

/** "сегодня, 26 ноября", "вчера, 25 ноября" or "24 ноября" for a day header. */
export function dayHead(day: string) {
  const s = new Date(day + 'T12:00:00').toLocaleDateString(locale(), { day: 'numeric', month: 'long' })
  const today = todayStr()
  return day === today ? tr('сегодня, {day}', { day: s }) : day === addDays(today, -1) ? tr('вчера, {day}', { day: s }) : s
}

export const fmtTime = (iso: string) => new Date(iso).toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit' })

/** "17:08" for today, "4 ноя, 22:10" for other days (from a stored UTC timestamp). */
export function fmtStamp(iso: string) {
  const d = new Date(iso)
  return isoDay(d) === todayStr() ? fmtTime(iso) : `${fmtDay(isoDay(d))}, ${fmtTime(iso)}`
}
