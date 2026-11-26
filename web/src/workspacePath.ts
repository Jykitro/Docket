// Each browser document is bound to a single workspace, named by the first URL segment (/<workspace>/<project>…).
// A switch uses a new document.

// First URL segments owned by app pages or the server, never a workspace (the server's reservedSlugs in
// internal/database/catalog.go keeps workspace slugs out of them). A project whose slug is one of these keeps /p/<slug>.
const RESERVED = new Set(['p', 't', 'w', 'attention', 'integrations', 'mcp', 'trash', 'settings', 'oauth', 'register', 'recover', 'invite', 'api', 'healthz', 'assets'])

/** The invite token of /invite#<token> (or of an old /invite/<token> link), kept in memory only: it leaves the address
 * bar and history before the router reads the URL, so it shows in no screenshot, history entry or later request. */
export const inviteToken = (() => {
  const m = /^\/invite(?:\/([^/]+))?\/?$/.exec(location.pathname)
  if (!m) return ''
  const token = m[1] ?? location.hash.slice(1) // tokens are base64url: nothing to decode
  history.replaceState(history.state, '', '/invite')
  return token
})()

const first = location.pathname.split('/')[1]
/** This document's workspace slug; '' on pages outside a workspace and on old /w/<id>/ links (WorkspaceGate redirects). */
export const workspaceSlug = first && !RESERVED.has(first) ? first : ''

/** Public URL of an app path: /p/<slug>… drops /p/ and, in a workspace document, gains /<workspace> (pages outside a workspace keep their URL). */
export function workspacePath(path: string) {
  if (!path.startsWith('/')) return path
  const slug = /^\/p\/([^/?#]+)/.exec(path)?.[1]
  if (slug && !RESERVED.has(slug)) path = path.slice(2)
  if (!workspaceSlug || /^\/(?:register$|recover$|invite(?:$|\/))/.test(path)) return path
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

/** An API path in this document's workspace: /api/projects → /api/workspaces/<slug>/projects. */
export function workspaceApi(path: string) {
  if (!workspaceSlug || !path.startsWith('/api/') || /^\/api\/(auth|admin|users|workspaces)(?:\/|$)/.test(path))
    return path
  return path.replace('/api/', `/api/workspaces/${workspaceSlug}/`)
}
