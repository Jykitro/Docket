import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errText, errorKind } from './api'
import { tr } from './i18n'
import { PlandilloLogo } from './PlandilloLogo'
import { ErrorPage, Select, btn, btnPri, cap } from './ui'
import { workspaceSlug } from './workspacePath'

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
  // A broken or stale link: nothing to approve, so explain it on a full page instead of a red line in an empty card.
  if (request.error && errorKind(request.error) === 'other') return <ErrorPage full kind="other" error={request.error}
    title={tr('Ссылка для подключения не работает')}
    hint={tr('Она устарела или открылась не полностью. Начните подключение заново в приложении, которое просит доступ.')}
    actions={<a className={btn} href="/">{tr('На главную')}</a>} />
  if (request.error) return <ErrorPage full error={request.error} />
  return <main className="min-h-screen grid place-items-center bg-bg p-4 text-tx">
    <section className="grid w-full max-w-md gap-4 rounded-lg border border-ln bg-sf p-6 shadow-pop">
      <PlandilloLogo />
      <h1 className="text-xl font-semibold">{tr('Подключение к Plandillo')}</h1>
      {request.data && <>
        <p className="break-words">{tr('Приложение «{name}» запрашивает доступ к задачам и страницам.', { name: request.data.client })}</p>
        <p className="min-w-0 break-all text-xs text-t2">{tr('Адрес возврата')} <code>{request.data.redirect_uri}</code></p>
        <div className="grid gap-1"><span className={cap}>{tr('Рабочее пространство')}</span>
          <Select block label={tr('Рабочее пространство')} value={workspace} onChange={setSelected} options={active.map(s => ({ value: String(s.id), label: s.name }))} />
        </div>
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
  const key = ['oauth-connections', workspaceSlug]
  const list = useQuery({ queryKey: key, queryFn: () => api.oauthConnections(workspaceSlug), enabled: !!workspaceSlug })
  const revoke = useMutation({ mutationFn: api.revokeOAuth, onSuccess: () => qc.invalidateQueries({ queryKey: key }) })
  const error = list.error || revoke.error
  if (!list.data?.length && !error) return null
  return <section className="overflow-hidden rounded-xl border border-ln bg-sf" aria-labelledby="apps-title">
    <h2 id="apps-title" className="px-4 pt-3.5 pb-3 text-[15px] font-semibold">{tr('Подключённые приложения')}</h2>
    {!!list.data?.length && <div className="overflow-x-auto"><table className="w-full text-[13px]">
      <thead><tr className="border-t border-ln bg-sf2 text-left text-xs text-t3">
        <th scope="col" className="h-[30px] px-4 font-medium">{tr('Приложение')}</th>
        <th scope="col" className="px-3 font-medium">{tr('Права')}</th>
        <th scope="col" className="px-4"><span className="sr-only">{tr('Действие')}</span></th>
      </tr></thead>
      <tbody>{list.data.map(c => <tr key={c.id} className="border-t border-ln">
        <td className="px-4 py-2 font-medium break-all">{c.name}</td>
        <td className="px-3 py-2 whitespace-nowrap text-t2">{c.scope.split(' ').includes('mcp:write') ? tr('Чтение и запись') : tr('Только чтение')}</td>
        <td className="px-4 py-2 text-right"><button className={btn} disabled={revoke.isPending} onClick={() => revoke.mutate(c.id)}>{tr('Отключить')}</button></td>
      </tr>)}</tbody>
    </table></div>}
    {error && <p role="alert" className="px-4 pb-3 text-sm text-rd">{errText(error)}</p>}
  </section>
}
