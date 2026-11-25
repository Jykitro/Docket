import { type ReactNode, useEffect, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, errText, type Space, useMe } from './api'
import { tr } from './i18n'
import { Icon } from './icons'
import { ProjectIconPicker } from './ProjectIconPicker'
import { btn, btnPri, cap } from './ui'
import { workspaceId } from './workspacePath'

const useSpaces = () => useQuery({ queryKey: ['workspaces'], queryFn: api.spaces })
const field = 'min-w-0 rounded-md border border-ln px-2 py-1.5 bg-sf'
export function WorkspaceGate({ children }: { children: ReactNode }) {
  const spaces = useSpaces()
  const first = spaces.data?.find((s) => !s.archived_at)
  const current = spaces.data?.find((s) => s.id === workspaceId && !s.archived_at)
  useEffect(() => {
    if (!workspaceId && first) location.replace('/w/' + first.id + location.pathname + location.search + location.hash)
  }, [first])
  if (spaces.isError) return <p role="alert">{errText(spaces.error)}</p>
  if (!workspaceId || !spaces.data) return null
  if (!current)
    return (
      <main className="mx-auto p-8 max-w-md">
        <h1>{tr('Пространство недоступно')}</h1>
        {first && (
          <a className={btn} href={'/w/' + first.id + '/'}>
            {tr('Открыть своё пространство')}
          </a>
        )}
      </main>
    )
  return <>{children}</>
}
export function WorkspaceSelector({ rail, onSettings }: { rail: boolean; onSettings: () => void }) {
  const list = useSpaces().data ?? []
  const current = list.find((s) => s.id === workspaceId)
  return (
    <div className="flex min-w-0 items-center gap-1 p-1">
      <button
        className={btn}
        aria-label={tr('Настройки пространства')}
        title={tr('Настройки пространства')}
        onClick={onSettings}
      >
        {current?.has_icon ? (
          <img alt="" className="size-4" src={`/api/workspaces/${current.id}/icon?v=${current.updated_at}`} />
        ) : (
          <Icon name={current?.icon ?? 'board'} />
        )}
      </button>
      {!rail && (
        <select
          className="min-w-0 flex-1 truncate bg-sf text-sm font-medium"
          aria-label={tr('Рабочее пространство')}
          value={workspaceId}
          onChange={(e) => location.assign('/w/' + e.target.value + '/')}
        >
          {list
            .filter((s) => !s.archived_at)
            .map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
        </select>
      )}
    </div>
  )
}
export function WorkspaceSettings() {
  const list = useSpaces()
  const qc = useQueryClient()
  const current = list.data?.find((s) => s.id === workspaceId)
  const selfhost = useMe().data?.mode === 'selfhost'
  const [name, setName] = useState(current?.name ?? '')
  const [newName, setNewName] = useState('')
  const [removing, setRemoving] = useState<Space>()
  const [confirm, setConfirm] = useState('')
  const [password, setPassword] = useState('')
  const change = useMutation({
    mutationFn: (run: () => Promise<unknown>) => run(),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['workspaces'] }),
  })
  if (!current) return null
  const patch = (data: Parameters<typeof api.changeSpace>[1]) =>
    change.mutateAsync(() => api.changeSpace(current.id, data))
  return (
    <section className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-2 border-t border-ln pt-3">
      <h3 className={cap}>{tr('Рабочее пространство')}</h3>
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          change.mutate(() => api.changeSpace(current.id, { name: name.trim() }))
        }}
      >
        <input
          className={field + ' grow'}
          aria-label={tr('Название пространства')}
          value={name}
          required
          maxLength={80}
          onChange={(e) => setName(e.target.value)}
        />
        <button className={btn} disabled={change.isPending || !name.trim() || name.trim() === current.name}>
          {tr('Сохранить')}
        </button>
      </form>
      <ProjectIconPicker
        icon={current.icon}
        preview={current.has_icon ? `/api/workspaces/${current.id}/icon?v=${current.updated_at}` : undefined}
        onSelect={patch}
      />
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          change.mutate(async () => {
            const sp = await api.createSpace(newName.trim())
            location.assign('/w/' + sp.id + '/')
          })
        }}
      >
        <input
          className={field + ' grow'}
          aria-label={tr('Новое пространство')}
          placeholder={tr('Новое пространство')}
          value={newName}
          required
          maxLength={80}
          onChange={(e) => setNewName(e.target.value)}
        />
        <button className={btnPri} disabled={change.isPending || !newName.trim()}>
          {tr('Создать')}
        </button>
      </form>
      {(list.data ?? []).filter((s) => !s.archived_at).length > 1 && (
        <button
          className={btn}
          onClick={() =>
            change.mutate(async () => {
              await api.changeSpace(current.id, { action: 'archive' })
              location.assign('/')
            })
          }
        >
          {tr('Архивировать пространство')}
        </button>
      )}
      {(list.data ?? [])
        .filter((s) => s.archived_at)
        .map((s) => (
          <div className="flex flex-wrap items-center gap-2" key={s.id}>
            <span className="min-w-0 grow truncate">{s.name}</span>
            <button className={btn} onClick={() => change.mutate(() => api.changeSpace(s.id, { action: 'restore' }))}>
              {tr('Восстановить')}
            </button>
            <button
              className={btn}
              onClick={() => {
                setRemoving(s)
                setConfirm('')
                setPassword('')
              }}
            >
              {tr('Удалить навсегда')}
            </button>
          </div>
        ))}
      {removing && (
        <form
          className="grid gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            change.mutate(async () => {
              await api.deleteSpace(removing.id, confirm, password)
              setRemoving(undefined)
              setPassword('')
            })
          }}
        >
          <label>
            {tr('Для удаления введите название')} <strong>{removing.name}</strong>
            <input
              className={field + ' w-full'}
              autoComplete="off"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          </label>
          {selfhost && (
            <label>
              {tr('Пароль')}
              <input
                className={field + ' w-full'}
                required
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </label>
          )}
          <div className="flex gap-2">
            <button
              className={btn}
              type="button"
              onClick={() => {
                setRemoving(undefined)
                setPassword('')
              }}
            >
              {tr('Отмена')}
            </button>
            <button className={btn} disabled={change.isPending || confirm !== removing.name}>
              {tr('Удалить навсегда')}
            </button>
          </div>
        </form>
      )}
      {change.isError && <p role="alert">{errText(change.error)}</p>}
    </section>
  )
}
