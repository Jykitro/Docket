// Each browser document is bound to a single workspace. A switch uses a new document.
export const workspaceId = Number(/^\/w\/([1-9]\d*)(?:\/|$)/.exec(location.pathname)?.[1] ?? 0)
export function workspacePath(path: string) {
  if (!workspaceId || !path.startsWith('/') || /^\/(?:w\/|register$|recover$|invite\/)/.test(path)) return path
  return `/w/${workspaceId}${path}`
}
export function workspaceApi(path: string) {
  if (!workspaceId || !path.startsWith('/api/') || /^\/api\/(auth|admin|users|workspaces)(?:\/|$)/.test(path))
    return path
  return path.replace('/api/', `/api/workspaces/${workspaceId}/`)
}
