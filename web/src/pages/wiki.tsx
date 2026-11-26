import { createFileRoute } from '@tanstack/react-router'

// Rendered by the board route above it (pages/board.tsx), which stays mounted when the wiki tab opens.
export const Route = createFileRoute('/_shell/p/$slug/wiki/{-$pageId}')({})
