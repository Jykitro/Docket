// Self-host account block of the settings dialog: who you are, logout, password change and, for admins,
// users and invites. Rendered by AppSettings only in self-host mode.
import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, type AuthUser, api, ruError } from './api'
import { Icon } from './icons'
import { ConfirmButton, btn, btnPri, cap } from './ui'

const input = 'h-9 w-full min-w-0 border border-ln rounded-md bg-sf px-2.5 text-sm text-tx focus:border-ring focus:ring-[3px] focus:ring-ring/50 focus:outline-none'
const item = 'flex items-center gap-3 min-h-10 px-3 rounded-md border border-ln bg-sf2 text-sm'
const day = (iso: string) => new Date(iso).toLocaleDateString('ru-RU')
const when = (iso: string) => new Date(iso).toLocaleString('ru-RU', { dateStyle: 'medium', timeStyle: 'short' })

function ChangePassword({ username }: { username: string }) {
  const [old, setOld] = useState('')
  const [next, setNext] = useState('')
  const change = useMutation({
    mutationFn: () => api.changePassword(old, next),
    onSuccess: () => { setOld(''); setNext('') },
  })
  return (
    <form className="flex flex-col gap-1.5" onSubmit={(e) => { e.preventDefault(); if (!change.isPending) change.mutate() }}>
      <span className={cap}>Смена пароля</span>
      {/* Lets password managers file the new password under this account. */}
      <input className="sr-only" type="text" autoComplete="username" readOnly tabIndex={-1} aria-hidden="true" value={username} />
      <input className={input} type="password" required autoComplete="current-password" maxLength={128} placeholder="Текущий пароль" aria-label="Текущий пароль" value={old} onChange={(e) => { change.reset(); setOld(e.target.value) }} />
      <input className={input} type="password" required autoComplete="new-password" minLength={10} maxLength={128} placeholder="Новый пароль (не короче 10 символов)" aria-label="Новый пароль" value={next} onChange={(e) => { change.reset(); setNext(e.target.value) }} />
      <div className="flex items-center gap-3">
        <button type="submit" className={btn} disabled={change.isPending}>Сменить пароль</button>
        {change.isSuccess && <span className="text-[13px] text-t2" role="status">Пароль изменён</span>}
        {change.isError && <span className="text-rd text-[13px]" role="alert">{ruError(change.error)}</span>}
      </div>
    </form>
  )
}

function Invites() {
  const qc = useQueryClient()
  const pending = useQuery({ queryKey: ['admin', 'invites'], queryFn: api.adminInvites })
  const [copied, setCopied] = useState<'done' | 'manual' | null>(null)
  const create = useMutation({
    mutationFn: api.createInvite,
    onSuccess: () => { setCopied(null); return qc.invalidateQueries({ queryKey: ['admin', 'invites'] }) }, // a new link drops the old copy state
  })
  const revoke = useMutation({
    // 404: already used, expired or revoked elsewhere; the refreshed list shows it.
    mutationFn: (id: number) => api.revokeInvite(id).catch((e) => { if (!(e instanceof ApiError && e.code === 'not_found')) throw e }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['admin', 'invites'] }),
  })
  const link = create.data && location.origin + create.data.url
  const field = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (!copied) return
    const t = setTimeout(() => setCopied(null), copied === 'done' ? 2000 : 4000)
    return () => clearTimeout(t)
  }, [copied])
  // navigator.clipboard is missing on plain-HTTP origins (a LAN IP), so fall back to execCommand, then to a manual-copy hint.
  const copy = async () => {
    try { await navigator.clipboard.writeText(link!); return setCopied('done') } catch { /* try the legacy path */ }
    field.current?.select()
    try { if (document.execCommand('copy')) return setCopied('done') } catch { /* leave it selected */ }
    setCopied('manual')
  }
  return (
    <div className="flex flex-col gap-1.5">
      <span className={cap}>Приглашения</span>
      <div>
        <button type="button" className={btnPri} disabled={create.isPending} onClick={() => create.mutate()}><Icon name="plus" size={15} sw={2} />Создать приглашение</button>
      </div>
      <span className="text-xs text-t3">Новое приглашение не отменяет прежние. Полная ссылка показывается только один раз.</span>
      {create.isError && <div className="text-rd text-[13px]" role="alert">{ruError(create.error)}</div>}
      {link && create.data && (
        <div className="flex flex-col gap-1.5">
          <div className="flex gap-2">
            <input ref={field} className={input + ' font-mono text-[12.5px]'} readOnly value={link} aria-label="Ссылка-приглашение" onFocus={(e) => e.currentTarget.select()} />
            <button type="button" className={btn} onClick={copy}>{copied === 'done' ? 'Скопировано' : 'Копировать'}</button>
          </div>
          {copied === 'manual' && <span className="text-xs text-t2" role="status">Выделено — нажмите Ctrl+C</span>}
          <span className="text-xs text-t3">Действует до {when(create.data.expires_at)}. Ссылка показывается один раз: сервер хранит только её хеш, потом её не восстановить.</span>
        </div>
      )}
      {pending.data?.map((i) => (
        <div key={i.id} className={item + ' flex-wrap'}>
          <span className="grow">Создано {day(i.created_at)}</span>
          <span className="text-[12.5px] text-t3">до {when(i.expires_at)}</span>
          <ConfirmButton className="shrink-0 p-0 text-[12.5px] text-t3 hover:text-rd" label="Отозвать" ariaLabel={`Отозвать приглашение от ${day(i.created_at)}`} armed="Отозвать приглашение?" disabled={revoke.isPending} onConfirm={() => revoke.mutate(i.id)} />
        </div>
      ))}
      {revoke.isError && <div className="text-rd text-[13px]" role="alert">{ruError(revoke.error)}</div>}
      {pending.isPending && <span className="px-3 text-[12.5px] text-t3">Загрузка…</span>}
      {pending.data?.length === 0 && <span className="px-3 text-[12.5px] text-t3">Активных приглашений нет</span>}
    </div>
  )
}

function Users({ me }: { me: AuthUser }) {
  const qc = useQueryClient()
  const users = useQuery({ queryKey: ['admin', 'users'], queryFn: api.adminUsers })
  const remove = useMutation({
    mutationFn: api.deleteUser,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'users'] }),
  })
  return (
    <>
      <div className="flex flex-col gap-1.5">
        <span className={cap}>Пользователи</span>
        {users.isPending && <span className="px-3 text-[12.5px] text-t3">Загрузка…</span>}
        {users.data?.map((u) => (
          <div key={u.id} className={item}>
            <span className="grow min-w-0 truncate">{u.username}{u.id === me.id && <span className="ml-2 text-xs text-t3">вы</span>}</span>
            <span className="shrink-0 text-[12.5px] text-t3">{day(u.created_at)}</span>
            {u.id !== me.id && (
              <ConfirmButton className="shrink-0 max-w-[65%] p-0 text-right text-[12.5px] text-t3 hover:text-rd" label="Удалить" ariaLabel={`Удалить пользователя ${u.username}`} armed={`Удалить «${u.username}» с данными?`} disabled={remove.isPending} onConfirm={() => remove.mutate(u.id)} />
            )}
          </div>
        ))}
        {users.isError && <div className="text-rd text-[13px]" role="alert">{ruError(users.error)}</div>}
        {remove.isError && <div className="text-rd text-[13px]" role="alert">{ruError(remove.error)}</div>}
      </div>
      <Invites />
    </>
  )
}

export function AccountSection({ user }: { user: AuthUser }) {
  const logout = useMutation({ mutationFn: api.logout }) // api.logout itself sends the user back to the login screen
  return (
    <>
      <div className="flex flex-col gap-1.5">
        <span className={cap}>Аккаунт</span>
        <div className={item}>
          <span className="grow min-w-0 truncate">{user.username}{user.is_admin && <span className="ml-2 text-xs text-t3">администратор</span>}</span>
          <button type="button" className={btn} disabled={logout.isPending} onClick={() => logout.mutate()}>Выйти</button>
        </div>
        {logout.isError && <div className="text-rd text-[13px]" role="alert">{ruError(logout.error)}</div>}
      </div>
      <ChangePassword username={user.username} />
      {user.is_admin && <Users me={user} />}
    </>
  )
}
