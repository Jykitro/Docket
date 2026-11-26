import { createFileRoute } from '@tanstack/react-router'
import { useShell } from '../App'
import { SettingsPage } from '../SettingsPage'

export const Route = createFileRoute('/_shell/settings/{-$section}')({ component: Page })

function Page() {
  const { theme } = useShell()
  return <SettingsPage section={Route.useParams().section ?? 'profile'} theme={theme.pref} onTheme={theme.set} />
}
