// Workspaces: the gate that binds a document to one workspace, the sidebar switcher, and the workspace settings pages.
// Each browser document belongs to one workspace (see workspacePath.ts), so switching loads a new document.
import { type ReactNode, useEffect, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, errText, type Space, useMe, useProfile } from './api'
import { locale, tr } from './i18n'
import { Icon } from './icons'
import { ProjectIconPicker } from './ProjectIconPicker'
import { Link } from './router'
import { Dialog, MENUITEM, MENUSEP, Menu, SettingsRow, SettingsSection, btn, btnPri, cap, textInput } from './ui'
import { workspaceApi, workspaceId } from './workspacePath'

export const useSpaces = () => useQuery({ queryKey: ['workspaces'], queryFn: api.spaces })
const open = (id: number) => location.assign('/w/' + id + '/')
export const SPACE_NAME_MAX = 80

export function WorkspaceGate({ children }: { children: ReactNode }) {
  const spaces = useSpaces()
  const first = spaces.data?.find((s) => !s.archived_at)
  const current = spaces.data?.find((s) => s.id === workspaceId && !s.archived_at)
  useEffect(() => {
    if (!workspaceId && first) location.replace('/w/' + first.id + location.pathname + location.search + location.hash)
  }, [first])
  if (spaces.isError) return <p role="alert" className="p-8 text-rd">{errText(spaces.error)}</p>
  if (!workspaceId || !spaces.data) return null
  if (!current)
    return (
      <main className="grid min-h-screen place-items-center bg-bg p-6 text-tx">
        <div className="flex max-w-sm flex-col items-center gap-3 text-center">
          <h1 className="text-lg font-semibold">{tr('Пространство недоступно')}</h1>
          <p className="text-sm text-t2">{tr('Его архивировали или у вас нет к нему доступа.')}</p>
          {first && <a className={btnPri} href={'/w/' + first.id + '/'}>{tr('Открыть своё пространство')}</a>}
        </div>
      </main>
    )
  return <>{children}</>
}

/** The workspace mark: the uploaded image, or the chosen glyph on the brand colour. */
export function SpaceIcon({ space, size = 32 }: { space?: Pick<Space, 'id' | 'icon' | 'has_icon' | 'updated_at'>; size?: number }) {
  const box = { width: size, height: size }
  if (space?.has_icon) return <img alt="" style={box} className="shrink-0 rounded-lg object-cover" src={`/api/workspaces/${space.id}/icon?v=${space.updated_at}`} />
  return <span style={box} className="grid shrink-0 place-items-center rounded-lg bg-ink text-inkt"><Icon name={space?.icon || 'board'} size={Math.round(size * 0.55)} sw={2} /></span>
}

export function WorkspaceSwitcher({ rail }: { rail: boolean }) {
  const spaces = useSpaces().data ?? []
  const list = spaces.filter((s) => !s.archived_at)
  const current = spaces.find((s) => s.id === workspaceId)
  const owner = useProfile().data?.name
  const [creating, setCreating] = useState(false)
  const trigger = rail
    ? 'grid size-10 place-items-center rounded-lg hover:bg-sk aria-expanded:bg-sk'
    : 'flex w-full items-center gap-2.5 rounded-lg p-1.5 text-left hover:bg-sk aria-expanded:bg-sk'
  return (
    <>
      <Menu block className={trigger} ariaLabel={tr('Сменить пространство')} title={rail ? current?.name : undefined} label={
        <>
          <SpaceIcon space={current} />
          {!rail && (
            <>
              <span className="flex min-w-0 grow flex-col leading-tight">
                <b className="truncate text-sm font-semibold">{current?.name ?? tr('Пространство')}</b>
                <span className="truncate text-xs text-t3">{owner ?? tr('Рабочее пространство')}</span>
              </span>
              <span className="text-t3"><Icon name="chevUD" size={16} /></span>
            </>
          )}
        </>
      }>
        {(close) => (
          <div className="w-60">
            <div className={cap + ' px-2 pt-1 pb-1.5'}>{tr('Пространства')}</div>
            {list.map((s) => (
              <button key={s.id} type="button" role="menuitemradio" aria-checked={s.id === workspaceId} className={MENUITEM + ' h-9'}
                onClick={() => { close(); if (s.id !== workspaceId) open(s.id) }}>
                <SpaceIcon space={s} size={22} />
                <span className="min-w-0 grow truncate">{s.name}</span>
                {s.id === workspaceId && <Icon name="check" size={14} sw={2.2} />}
              </button>
            ))}
            <div className={MENUSEP} />
            <button type="button" className={MENUITEM} onClick={() => { close(); setCreating(true) }}>
              <span className="grid size-[22px] place-items-center rounded-md border border-dashed border-ln2 text-t2"><Icon name="plus" size={13} sw={2} /></span>
              {tr('Новое пространство')}
            </button>
            <Link to="/settings/workspace" className={MENUITEM + ' text-tx no-underline'} onClick={close}>
              <span className="grid size-[22px] place-items-center text-t2"><Icon name="sliders" size={15} /></span>
              {tr('Настройки пространства')}
            </Link>
          </div>
        )}
      </Menu>
      {creating && <NewWorkspaceDialog onClose={() => setCreating(false)} />}
    </>
  )
}

function NewWorkspaceDialog({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const create = useMutation({ mutationFn: () => api.createSpace(name.trim()), onSuccess: (sp) => open(sp.id) })
  return (
    <Dialog title={tr('Новое пространство')} onClose={onClose}>
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); if (name.trim() && !create.isPending) create.mutate() }}>
        <p className="text-sm text-t2">{tr('У пространства свои проекты, вики, интеграции и токен MCP. Переключаться можно в любой момент.')}</p>
        <label className="flex flex-col gap-1.5">
          <span className={cap}>{tr('Название')}</span>
          <input className={textInput} autoFocus required maxLength={SPACE_NAME_MAX} placeholder={tr('Например, Работа или Дом')} value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        {create.isError && <p className="text-[13px] text-rd" role="alert">{errText(create.error)}</p>}
        <div className="flex justify-end gap-2">
          <button type="button" className={btn} onClick={onClose}>{tr('Отмена')}</button>
          <button className={btnPri} disabled={!name.trim() || create.isPending}>{tr('Создать пространство')}</button>
        </div>
      </form>
    </Dialog>
  )
}

function useChangeSpace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (run: () => Promise<unknown>) => run(),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['workspaces'] }),
  })
}

function NameForm({ space }: { space: Space }) {
  const change = useChangeSpace()
  const [name, setName] = useState(space.name)
  return (
    <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); change.mutate(() => api.changeSpace(space.id, { name: name.trim() })) }}>
      <input className={textInput} aria-label={tr('Название пространства')} value={name} required maxLength={SPACE_NAME_MAX} onChange={(e) => setName(e.target.value)} />
      <button className={btnPri} disabled={change.isPending || !name.trim() || name.trim() === space.name}>{tr('Сохранить')}</button>
      {change.isError && <p className="text-[13px] text-rd" role="alert">{errText(change.error)}</p>}
    </form>
  )
}

/** Settings → Workspace → General: name, icon, storage, data, archive. */
export function WorkspaceGeneral() {
  const spaces = useSpaces()
  const current = spaces.data?.find((s) => s.id === workspaceId)
  const storage = useProfile().data?.storage
  const change = useChangeSpace()
  if (!current) return null
  const canArchive = (spaces.data ?? []).filter((s) => !s.archived_at).length > 1
  return (
    <>
      <SettingsSection title={tr('Название и иконка')}>
        <SettingsRow label={tr('Название')} hint={tr('Видно в переключателе и в приложениях, подключённых через MCP.')}>
          <NameForm key={current.name} space={current} />
        </SettingsRow>
        <SettingsRow label={tr('Иконка')} hint={tr('Глиф на фирменном цвете или своё изображение.')}>
          <div className="flex items-start gap-3">
            <SpaceIcon space={current} size={40} />
            <ProjectIconPicker icon={current.icon} preview={current.has_icon ? `/api/workspaces/${current.id}/icon?v=${current.updated_at}` : undefined}
              onSelect={(data) => change.mutateAsync(() => api.changeSpace(current.id, data))} />
          </div>
        </SettingsRow>
        {storage && (
          <SettingsRow label={tr('Хранилище')} hint={tr('Лимит демо-пространства.')}>
            <div className="h-2 overflow-hidden rounded-full bg-sk"><div className="h-full rounded-full bg-ink" style={{ width: `${Math.min(100, (storage.used / storage.limit) * 100)}%` }} /></div>
            <span className="text-[13px] text-t2">{tr('Хранилище демо: {used} из {limit} МБ', {
              used: (storage.used / 1_000_000).toLocaleString(locale(), { maximumFractionDigits: 2 }),
              limit: storage.limit / 1_000_000,
            })}</span>
          </SettingsRow>
        )}
      </SettingsSection>
      <SettingsSection title={tr('Данные')}>
        <SettingsRow label={tr('Экспорт')} hint={tr('Все проекты, задачи, заметки и вики этого пространства одним JSON-файлом.')}>
          <div><a className={btn} href={workspaceApi('/api/export')} download><Icon name="down" size={15} />{tr('Скачать JSON')}</a></div>
        </SettingsRow>
        <SettingsRow label={tr('Корзина')} hint={tr('Удалённые задачи и проекты можно восстановить.')}>
          <div><Link to="/trash" className={btn}><Icon name="trash" size={15} />{tr('Открыть корзину')}</Link></div>
        </SettingsRow>
        <SettingsRow label={tr('Интеграции')} hint={tr('MCP, Code Mode, токен и доступ для агентов.')}>
          <div><Link to="/integrations" className={btn}><Icon name="plug" size={15} />{tr('Открыть интеграции')}</Link></div>
        </SettingsRow>
      </SettingsSection>
      {canArchive && (
        <SettingsSection title={tr('Опасная зона')} danger>
          <SettingsRow label={tr('Архивировать пространство')} hint={tr('Пространство пропадёт из переключателя. Его можно восстановить на странице «Все пространства».')}>
            <div><button type="button" className={btn + ' text-rd'} disabled={change.isPending}
              onClick={() => change.mutate(async () => { await api.changeSpace(current.id, { action: 'archive' }); location.assign('/') })}>{tr('Архивировать')}</button></div>
          </SettingsRow>
        </SettingsSection>
      )}
      {change.isError && <p className="text-[13px] text-rd" role="alert">{errText(change.error)}</p>}
    </>
  )
}

/** Settings → Workspace → All workspaces: switch, create, restore and delete archived ones. */
export function WorkspaceList() {
  const spaces = useSpaces()
  const change = useChangeSpace()
  const selfhost = useMe().data?.mode === 'selfhost'
  const [newName, setNewName] = useState('')
  const [removing, setRemoving] = useState<Space>()
  const [confirm, setConfirm] = useState('')
  const [password, setPassword] = useState('')
  const active = (spaces.data ?? []).filter((s) => !s.archived_at)
  const archived = (spaces.data ?? []).filter((s) => s.archived_at)
  const row = 'flex min-h-14 items-center gap-3 border-t border-ln px-5 py-2.5 first:border-t-0'
  const stopRemoving = () => { setRemoving(undefined); setPassword(''); change.reset() }
  return (
    <>
      <SettingsSection title={tr('Ваши пространства')}>
        {active.map((s) => (
          <div key={s.id} className={row}>
            <SpaceIcon space={s} />
            <span className="min-w-0 grow truncate text-sm font-medium">{s.name}</span>
            {s.id === workspaceId ? <span className="rounded-md bg-acs px-2 py-0.5 text-xs font-medium text-act">{tr('Текущее')}</span>
              : <button type="button" className={btn} onClick={() => open(s.id)}>{tr('Открыть')}</button>}
          </div>
        ))}
        <form className={row + ' rounded-b-xl bg-sf2'} onSubmit={(e) => { e.preventDefault(); change.mutate(async () => open((await api.createSpace(newName.trim())).id)) }}>
          <input className={textInput} aria-label={tr('Новое пространство')} placeholder={tr('Новое пространство')} value={newName} required maxLength={SPACE_NAME_MAX} onChange={(e) => setNewName(e.target.value)} />
          <button className={btnPri} disabled={change.isPending || !newName.trim()}><Icon name="plus" size={15} sw={2} />{tr('Создать')}</button>
        </form>
      </SettingsSection>
      {archived.length > 0 && (
        <SettingsSection title={tr('Архив')} hint={tr('Архивные пространства не видны в переключателе. Удаление стирает их данные безвозвратно.')}>
          {archived.map((s) => (
            <div key={s.id} className={row + ' flex-wrap'}>
              <span className="opacity-60"><SpaceIcon space={s} /></span>
              <span className="min-w-0 grow truncate text-sm">{s.name}</span>
              <button type="button" className={btn} onClick={() => change.mutate(() => api.changeSpace(s.id, { action: 'restore' }))}>{tr('Восстановить')}</button>
              <button type="button" className={btn + ' text-rd'} onClick={() => { change.reset(); setRemoving(s); setConfirm(''); setPassword('') }}>{tr('Удалить навсегда')}</button>
            </div>
          ))}
        </SettingsSection>
      )}
      {removing && (
        <Dialog title={tr('Удалить «{name}» навсегда?', { name: removing.name })} onClose={stopRemoving}>
          <form className="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); change.mutate(async () => { await api.deleteSpace(removing.id, confirm, password); stopRemoving() }) }}>
            <p className="text-sm text-t2">{tr('Проекты, задачи и вики этого пространства будут удалены без возможности восстановления.')}</p>
            <label className="flex flex-col gap-1.5">
              <span className={cap}>{tr('Для удаления введите название')} <b className="text-tx">{removing.name}</b></span>
              <input className={textInput} autoComplete="off" autoFocus value={confirm} onChange={(e) => setConfirm(e.target.value)} />
            </label>
            {selfhost && (
              <label className="flex flex-col gap-1.5">
                <span className={cap}>{tr('Пароль')}</span>
                <input className={textInput} required type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
              </label>
            )}
            {change.isError && <p className="text-[13px] text-rd" role="alert">{errText(change.error)}</p>}
            <div className="flex justify-end gap-2">
              <button className={btn} type="button" onClick={stopRemoving}>{tr('Отмена')}</button>
              <button className={btnPri + ' border-rd bg-rd hover:bg-rd/90'} disabled={change.isPending || confirm !== removing.name}>{tr('Удалить навсегда')}</button>
            </div>
          </form>
        </Dialog>
      )}
      {!removing && change.isError && <p className="text-[13px] text-rd" role="alert">{errText(change.error)}</p>}
    </>
  )
}
