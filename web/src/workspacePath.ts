// Each browser document is bound to a single workspace, named by the first URL segment (/<workspace>/<project>…).
// A switch uses a new document.

// First URL segments owned by app pages or the server, never a workspace (the server's reservedSlugs in
// internal/database/catalog.go keeps workspace slugs out of them). A project whose slug is one of these keeps /p/<slug>.
const RESERVED = new Set(['p', 't', 'w', 'attention', 'integrations', 'mcp', 'trash', 'settings', 'oauth', 'register', 'recover', 'invite', 'api', 'healthz', 'assets'])

const first = location.pathname.split('/')[1]
/** This document's workspace slug; '' on pages outside a workspace and on old /w/<id>/ links (WorkspaceGate redirects). */
export const workspaceSlug = first && !RESERVED.has(first) ? first : ''

/** The workspace ID the API uses: from the list saved by the last load, or asked once before the app starts. */
export const workspaceId = workspaceSlug ? await resolveWorkspace(workspaceSlug) : 0

async function resolveWorkspace(slug: string) {
  const find = (rows: unknown) => (Array.isArray(rows) ? (rows as { id: number; slug: string }[]).find((s) => s.slug === slug)?.id : undefined)
  try {
    // The saved list may belong to an earlier sign-in; WorkspaceGate reloads when the server's list disagrees.
    const hit = find((JSON.parse(localStorage.getItem('plandillo-spaces') ?? 'null') as { rows?: unknown } | null)?.rows)
    if (hit) return hit
  } catch { /* storage can be blocked */ }
  try {
    const res = await fetch('/api/workspaces', { credentials: 'same-origin' })
    if (res.ok) return find(await res.json()) ?? 0
  } catch { /* offline: WorkspaceGate shows the error */ }
  return 0
}

/** Public URL of an app path: /p/<slug>… drops /p/ and, in a workspace document, gains /<workspace> (pages outside a workspace keep their URL). */
export function workspacePath(path: string) {
  if (!path.startsWith('/')) return path
  const slug = /^\/p\/([^/?#]+)/.exec(path)?.[1]
  if (slug && !RESERVED.has(slug)) path = path.slice(2)
  if (!workspaceSlug || /^\/(?:register$|recover$|invite\/)/.test(path)) return path
  return `/${workspaceSlug}${path.replace(/^\/(?=[?#]|$)/, '')}`
}

/** App path of a public pathname, the inverse of workspacePath: what the route tree matches. */
export function appPath(pathname: string) {
  const prefix = '/' + workspaceSlug
  if (workspaceSlug && (pathname === prefix || pathname.startsWith(prefix + '/'))) pathname = pathname.slice(prefix.length) || '/'
  else pathname = pathname.replace(/^\/w\/[1-9]\d*(?=\/|$)/, '') || '/' // an old link reaches WorkspaceGate, which moves it to the slug
  const head = pathname.split('/')[1]
  return head && !RESERVED.has(head) ? '/p' + pathname : pathname
}

export function workspaceApi(path: string) {
  if (!workspaceId || !path.startsWith('/api/') || /^\/api\/(auth|admin|users|workspaces)(?:\/|$)/.test(path))
    return path
  return path.replace('/api/', `/api/workspaces/${workspaceId}/`)
}
