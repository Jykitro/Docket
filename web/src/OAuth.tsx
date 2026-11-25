import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errText } from './api'
import { tr } from './i18n'
import { PlandilloLogo } from './PlandilloLogo'
import { btn, btnPri, cap } from './ui'
import { workspaceId } from './workspacePath'

export function OAuthConsent() {
  const request = useQuery({ queryKey: ['oauth-request', location.search], queryFn: () => api.oauthRequest(location.search), retry: false })
  const spaces = useQuery({ queryKey: ['workspaces'], queryFn: api.spaces })
  const [selected, setSelected] = useState('')
  const [write, setWrite] = useState(true)
  const active = spaces.data?.filter(s => !s.archived_at) ?? []
  const workspace = selected || String(active[0]?.id ?? '')
  const decision = useMutation({
    mutationFn: (allow: boolean) => api.oauthDecision(location.search, Number(workspace), allow, !!request.data?.write && write),
    onSuccess: result => location.replace(result.redirect),
    gcTime: 0,
  })
  const error = request.error || spaces.error || decision.error
  return <main className="min-h-screen grid place-items-center bg-bg p-4 text-tx">
    <section className="grid w-full max-w-md gap-4 rounded-lg border border-ln bg-sf p-6 shadow-pop">
      <PlandilloLogo />
      <h1 className="text-xl font-semibold">{tr('Подключение к Plandillo')}</h1>
      {request.data && <>
        <p className="break-words">{tr('Приложение «{name}» запрашивает доступ к задачам и страницам.', { name: request.data.client })}</p>
        <p className="min-w-0 break-all text-xs text-t2">{tr('Адрес возврата')} <code>{request.data.redirect_uri}</code></p>
        <label className="grid gap-1"><span className={cap}>{tr('Рабочее пространство')}</span>
          <select value={workspace} onChange={e => setSelected(e.target.value)} className="rounded-md border border-ln bg-sf p-2">
            {active.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
          </select>
        </label>
        {request.data.write ? <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={write} onChange={e => setWrite(e.target.checked)} />{tr('Разрешить изменения в этом пространстве')}
        </label> : <p>{tr('Только чтение')}</p>}
        <div className="flex flex-wrap justify-end gap-2">
          <button className={btn} disabled={decision.isPending} onClick={() => decision.mutate(false)}>{tr('Отмена')}</button>
          <button className={btnPri} disabled={!workspace || decision.isPending} onClick={() => decision.mutate(true)}>{tr('Разрешить')}</button>
        </div>
      </>}
      {request.isPending && <p role="status">{tr('Загрузка…')}</p>}
      {error && <p role="alert" className="text-sm text-rd">{errText(error)}</p>}
    </section>
  </main>
}

export function OAuthConnections() {
  const qc = useQueryClient()
  const key = ['oauth-connections', workspaceId]
  const list = useQuery({ queryKey: key, queryFn: () => api.oauthConnections(workspaceId), enabled: workspaceId > 0 })
  const revoke = useMutation({ mutationFn: api.revokeOAuth, onSuccess: () => qc.invalidateQueries({ queryKey: key }) })
  const error = list.error || revoke.error
  if (!list.data?.length && !error) return null
  return <section className="grid gap-3 rounded-xl border border-ln bg-sf p-5">
    <h2 className="font-semibold">{tr('Подключённые приложения')}</h2>
    {list.data?.map(c => <div key={c.id} className="flex flex-wrap items-center gap-2">
      <b className="grow break-all">{c.name}</b>
      <span className="text-sm text-t2">{c.scope.split(' ').includes('mcp:write') ? tr('Чтение и запись') : tr('Только чтение')}</span>
      <button className={btn} disabled={revoke.isPending} onClick={() => revoke.mutate(c.id)}>{tr('Отключить')}</button>
    </div>)}
    {error && <p role="alert" className="text-sm text-rd">{errText(error)}</p>}
  </section>
}
