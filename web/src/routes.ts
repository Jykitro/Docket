// The route tree (TanStack Router virtual file routes); files are in src/pages and each page loads as its own chunk.
// App paths of projects are /p/<slug>…; the public URL drops /p/ (see workspacePath.ts).
import { index, layout, rootRoute, route } from '@tanstack/virtual-file-routes'

export const routes = rootRoute('root.tsx', [
  route('/oauth/authorize', 'oauth.tsx'),
  layout('shell', 'shell.tsx', [
    index('today.tsx'),
    route('/attention', 'attention.tsx'),
    route('/integrations', 'integrations.tsx'),
    route('/mcp', 'mcp.tsx'), // old bookmarks
    route('/trash', 'trash.tsx'),
    route('/settings/{-$section}', 'settings.tsx'),
    route('/t/$code', 'task.tsx'),
    route('/p/$slug/settings', 'project-settings.tsx'),
    // The board stays mounted between its tabs and the wiki: the wiki route only marks the match.
    route('/p/$slug', 'board.tsx', [route('/wiki/{-$pageId}', 'wiki.tsx')]),
  ]),
])
