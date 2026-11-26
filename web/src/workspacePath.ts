// Each browser document is bound to a single workspace. A switch uses a new document.
export const workspaceId = Number(/^\/w\/([1-9]\d*)(?:\/|$)/.exec(location.pathname)?.[1] ?? 0)

// First URL segments owned by app pages or the server. Projects live at /<slug>; a project whose slug is one of these keeps /p/<slug>.
const RESERVED = new Set(['p', 't', 'w', 'attention', 'integrations', 'mcp', 'trash', 'settings', 'oauth', 'register', 'recover', 'invite', 'api', 'healthz', 'assets'])

/** Public URL of an app path: /p/<slug>… drops /p/ and, in a workspace document, gains /w/<id> (pages outside a workspace keep their URL). */
export function workspacePath(path: string) {
  if (!path.startsWith('/')) return path
  const slug = /^\/p\/([^/?#]+)/.exec(path)?.[1]
  if (slug && !RESERVED.has(slug)) path = path.slice(2)
  if (!workspaceId || /^\/(?:w\/|register$|recover$|invite\/)/.test(path)) return path
  return `/w/${workspaceId}${path}`
}

/** App path of a public pathname, the inverse of workspacePath: what the route tree matches. */
export function appPath(pathname: string) {
  pathname = pathname.replace(/^\/w\/[1-9]\d*(?=\/|$)/, '') || '/'
  const first = pathname.split('/')[1]
  return first && !RESERVED.has(first) ? '/p' + pathname : pathname
}

export function workspaceApi(path: string) {
  if (!workspaceId || !path.startsWith('/api/') || /^\/api\/(auth|admin|users|workspaces)(?:\/|$)/.test(path))
    return path
  return path.replace('/api/', `/api/workspaces/${workspaceId}/`)
}
