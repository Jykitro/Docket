// REST client, types and shared data hooks. Every list is a TanStack Query; SSE (live.ts) invalidates them.
import { useQueries, useQuery } from '@tanstack/react-query'

export type Category = 'todo' | 'doing' | 'done'

export type Project = {
  id: number; slug: string; key: string; name: string; icon: string; color: string; description: string
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
  id: number; project_id: number; number: number; code: string; parent_id: number | null; parent_code: string
  title: string; description_md: string; status_id: number; status: string; status_name: string
  status_category: Category; priority: 'P0' | 'P1' | 'P2'; area: string; planned_for: string | null
  position: number; custom: Record<string, string | number>; created_by: 'denis' | 'claude'
  created_at: string; updated_at: string; done_at: string | null; deleted_at: string | null; subtask_count: number; subtask_done: number
}
export type Activity = {
  id: number; task_id: number | null; project_id: number | null; actor: 'denis' | 'claude'; action: string
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
  project: string; title: string; status?: string; priority?: string; area?: string; planned_for?: string; parent?: string
}
export type Note = { id: number; task_id: number; author: 'denis' | 'claude'; body_md: string; created_at: string }
export type TaskDetail = Task & { subtasks: Task[]; notes: Note[]; activity: Activity[] }
export type Hit = {
  id: number; project_id: number; code: string; title: string; status: string; status_name: string; status_color: string; snippet?: string
}
export type SearchResult = {
  groups: { tasks: Hit[]; descriptions: Hit[]; projects: Project[] }; counts: Record<string, number>; took_ms: number
}

export type AuthUser = { id: number; username: string; is_admin: boolean; created_at: string }
export type Me = { mode: 'local' | 'selfhost'; setup?: boolean; user?: AuthUser }
export type AdminInvite = { id: number; created_at: string; expires_at: string }
export type NewInvite = { url: string; expires_at: string }

export class ApiError extends Error {
  constructor(public code: string, message: string) { super(message) }
}

// Server texts shown to the user in Russian; unknown ones fall back to the server's own message.
const RU: Record<string, string> = {
  'invalid username or password': 'Неверное имя пользователя или пароль',
  'current password is wrong': 'Неверный текущий пароль',
  'too many failed attempts, try again later': 'Слишком много попыток, подождите несколько минут.',
  'invalid setup code': 'Неверный код настройки',
  'username is taken': 'Имя пользователя уже занято',
  'invalid or expired invite': 'Приглашение недействительно или истекло',
  'username must be 2-32 characters: a-z, 0-9, _ . -, starting with a letter or digit':
    'Имя пользователя: 2-32 символа, a-z, 0-9, _ . -, начинается с буквы или цифры',
  'password must be 10-128 characters': 'Пароль должен быть от 10 до 128 символов',
  'cannot delete yourself': 'Нельзя удалить самого себя',
  'not logged in': 'Сессия истекла, войдите снова',
}
export const ruError = (e: unknown): string => (e instanceof Error ? RU[e.message] ?? e.message : String(e))

async function req<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => null)
  // Session expired: tell the gate (auth routes handle their own 401s, e.g. a wrong password).
  if (res.status === 401 && !url.startsWith('/api/auth/')) window.dispatchEvent(new Event('docket:unauthorized'))
  if (!res.ok) throw new ApiError(data?.error?.code ?? 'internal', data?.error?.message ?? res.statusText)
  return data as T
}

export const api = {
  me: () => req<Me>('GET', '/api/auth/me'),
  login: (username: string, password: string) => req<AuthUser>('POST', '/api/auth/login', { username, password }),
  // Same signal as an expired session: the gate drops cached data and re-asks who we are.
  // An already-expired session (401) is the state logout wants anyway, so it is not an error.
  logout: () => req<void>('POST', '/api/auth/logout')
    .catch((e) => { if (!(e instanceof ApiError && e.code === 'unauthorized')) throw e })
    .then(() => { window.dispatchEvent(new Event('docket:unauthorized')) }),
  setup: (code: string, username: string, password: string) => req<AuthUser>('POST', '/api/auth/setup', { code, username, password }),
  acceptInvite: (token: string, username: string, password: string) =>
    req<AuthUser>('POST', `/api/auth/invite/${encodeURIComponent(token)}`, { username, password }),
  changePassword: (old: string, next: string) => req<void>('PUT', '/api/auth/password', { old, new: next }),
  adminUsers: () => req<AuthUser[]>('GET', '/api/admin/users'),
  adminInvites: () => req<AdminInvite[]>('GET', '/api/admin/invites'),
  createInvite: () => req<NewInvite>('POST', '/api/admin/invites'),
  deleteUser: (id: number) => req<void>('DELETE', `/api/admin/users/${id}`),
  profile: () => req<Profile>('GET', '/api/profile'),
  setProfile: (name: string) => req<Profile>('PUT', '/api/profile', { name }),
  projects: () => req<Project[]>('GET', '/api/projects?include_archived=1'),
  createProject: (b: { name: string; key?: string; icon?: string; color?: string }) => req<Project>('POST', '/api/projects', b),
  statuses: (pid: number) => req<Status[]>('GET', `/api/projects/${pid}/statuses`),
  labels: (pid: number) => req<Label[]>('GET', `/api/projects/${pid}/labels`),
  fields: (pid: number) => req<Field[]>('GET', `/api/projects/${pid}/fields`),
  today: (date?: string) => req<TodayData>('GET', '/api/today' + (date ? `?date=${date}` : '')),
  tasks: (project: string) => req<Task[]>('GET', `/api/tasks?project=${encodeURIComponent(project)}&include_done=1`),
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
  mcpCalls: (filter: string) => req<McpCall[]>('GET', `/api/mcp/calls?filter=${filter}&limit=50`),
  clearMcpCalls: () => req<void>('DELETE', '/api/mcp/calls'),
  updateProject: (id: number, b: Record<string, unknown>) => req<Project>('PATCH', `/api/projects/${id}`, b),
  archiveProject: (id: number, archived: boolean) => req<Project>('POST', `/api/projects/${id}/archive`, { archived }),
  deleteProject: (id: number) => req<Project>('DELETE', `/api/projects/${id}`),
  restoreProject: (id: number) => req<Project>('POST', `/api/projects/${id}/restore`),
  trashProjects: () => req<Project[]>('GET', '/api/trash/projects'),
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
  trash: () => req<Task[]>('GET', '/api/trash'),
  purgeTrash: () => req<{ purged: number }>('DELETE', '/api/trash'),
  task: (ref: string) => req<TaskDetail>('GET', `/api/tasks/${ref}`),
  deleteTask: (ref: string) => req<{ trashed: number }>('DELETE', `/api/tasks/${ref}`),
  restoreTask: (ref: string) => req<Task>('POST', `/api/tasks/${ref}/restore`),
  addNote: (ref: string, body_md: string) => req<Note>('POST', `/api/tasks/${ref}/notes`, { body_md }),
  search: (q: string, scope = 'all') => req<SearchResult>('GET', `/api/search?q=${encodeURIComponent(q)}&scope=${scope}&limit=8`),
}

// ---- hooks ----

export const useMe = () => useQuery({ queryKey: ['me'], queryFn: api.me, retry: false })
export const useProjects = () => useQuery({ queryKey: ['projects'], queryFn: api.projects })
export const useProfile = () => useQuery({ queryKey: ['profile'], queryFn: api.profile })

/** Display name of an actor: claude stays "claude", the stored actor "denis" is whoever the profile says. */
export function useWho() {
  const name = useProfile().data?.name || 'Вы'
  return (actor: string) => (actor === 'claude' ? 'claude' : name)
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

/** Status colour: one of the four theme tokens or a custom hex. */
export const statusColor = (c: string) => (/^(bk|td|dg|dn)$/.test(c) ? `var(--${c})` : c)

/** Soft chip colours of a status (background and text), neutral for custom hex colours. */
export const statusChip = (c: string) =>
  /^(bk|td|dg|dn)$/.test(c) ? { background: `var(--${c}s)`, color: `var(--${c}t)` } : { background: 'var(--sk)', color: 'var(--t2)' }

const MONTHS = ['янв', 'фев', 'мар', 'апр', 'мая', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек']

const pad = (n: number) => String(n).padStart(2, '0')
export const isoDay = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
export const todayStr = () => isoDay(new Date())
export function addDays(day: string, n: number) {
  const d = new Date(day + 'T12:00:00')
  d.setDate(d.getDate() + n)
  return isoDay(d)
}

/** "Сегодня", "Завтра" or "4 ноя". */
export function fmtDay(day: string) {
  const today = todayStr()
  if (day === today) return 'Сегодня'
  if (day === addDays(today, 1)) return 'Завтра'
  const [, m, d] = day.split('-').map(Number)
  return `${d} ${MONTHS[m - 1]}`
}

export function fmtLongDay(day: string) {
  const d = new Date(day + 'T12:00:00')
  const s = d.toLocaleDateString('ru-RU', { weekday: 'long', day: 'numeric', month: 'long' })
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export const fmtTime = (iso: string) => new Date(iso).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })

/** "17:08" for today, "4 ноя, 22:10" for other days (from a stored UTC timestamp). */
export function fmtStamp(iso: string) {
  const d = new Date(iso)
  return isoDay(d) === todayStr() ? fmtTime(iso) : `${fmtDay(isoDay(d))}, ${fmtTime(iso)}`
}

export function plural(n: number, one: string, few: string, many: string) {
  const m = n % 100
  if (m >= 11 && m <= 14) return many
  const r = n % 10
  return r === 1 ? one : r >= 2 && r <= 4 ? few : many
}
