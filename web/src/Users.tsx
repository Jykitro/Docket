// Self-host account blocks of the settings pages: sign-in and password (Security) and, for admins, users and invites.
import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, type AuthUser, type Registration, api, errText } from './api'
import { PasskeySettings } from './Passkeys'
import { Icon } from './icons'
import { locale, tr } from './i18n'
import { ConfirmButton, Select, btn, btnPri, cap } from './ui'

const input = 'h-9 w-full min-w-0 border border-ln rounded-md bg-sf px-2.5 text-sm text-tx focus:border-ring focus:ring-[3px] focus:ring-ring focus:outline-none'
const item = 'flex items-center gap-3 min-h-10 px-3 rounded-md border border-ln bg-sf2 text-sm'
const day = (iso: string) => new Date(iso).toLocaleDateString(locale())
const when = (iso: string) => new Date(iso).toLocaleString(locale(), { dateStyle: 'medium', timeStyle: 'short' })

function ChangePassword({ username }: { username: string }) {
  const [old, setOld] = useState('')
  const [next, setNext] = useState('')
  const change = useMutation({
    mutationFn: () => api.changePassword(old, next),
    onSuccess: () => { setOld(''); setNext('') },
  })
  return (
    <form className="flex flex-col gap-1.5" onSubmit={(e) => { e.preventDefault(); if (!change.isPending) change.mutate() }}>
      <span className={cap}>{tr('Смена пароля')}</span>
      {/* Lets password managers file the new password under this account. */}
      <input className="sr-only" type="text" autoComplete="username" readOnly tabIndex={-1} aria-hidden="true" value={username} />
      <input className={input} type="password" required autoComplete="current-password" maxLength={128} placeholder={tr('Текущий пароль')} aria-label={tr('Текущий пароль')} value={old} onChange={(e) => { change.reset(); setOld(e.target.value) }} />
      <input className={input} type="password" required autoComplete="new-password" minLength={10} maxLength={128} placeholder={tr('Новый пароль (не короче 10 символов)')} aria-label={tr('Новый пароль')} value={next} onChange={(e) => { change.reset(); setNext(e.target.value) }} />
      <div className="flex items-center gap-3">
        <button type="submit" className={btn} disabled={change.isPending}>{tr('Сменить пароль')}</button>
        {change.isSuccess && <span className="text-[13px] text-t2" role="status">{tr('Пароль изменён')}</span>}
        {change.isError && <span className="text-rd text-[13px]" role="alert">{errText(change.error)}</span>}
      </div>
    </form>
  )
}

function Invites({ closed }: { closed: boolean }) {
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
      <span className={cap}>{tr('Приглашения')}</span>
      <div>
        <button type="button" className={btnPri} disabled={closed || create.isPending} onClick={() => create.mutate()}><Icon name="plus" size={15} sw={2} />{tr('Создать приглашение')}</button>
      </div>
      <span className="text-xs text-t3">{tr('Новое приглашение не отменяет прежние. Полная ссылка показывается только один раз.')}</span>
      {create.isError && <div className="text-rd text-[13px]" role="alert">{errText(create.error)}</div>}
      {link && create.data && (
        <div className="flex flex-col gap-1.5">
          <div className="flex gap-2">
            <input ref={field} className={input + ' font-mono text-[12.5px]'} readOnly value={link} aria-label={tr('Ссылка-приглашение')} onFocus={(e) => e.currentTarget.select()} />
            <button type="button" className={btn} onClick={copy}>{copied === 'done' ? tr('Скопировано') : tr('Копировать')}</button>
          </div>
          {copied === 'manual' && <span className="text-xs text-t2" role="status">{tr('Выделено — нажмите Ctrl+C')}</span>}
          <span className="text-xs text-t3">{tr('Действует до {when}. Ссылка показывается один раз: сервер хранит только её хеш, потом её не восстановить.', { when: when(create.data.expires_at) })}</span>
        </div>
      )}
      {pending.data?.map((i) => (
        <div key={i.id} className={item + ' flex-wrap'}>
          <span className="grow">{tr('Создано {day}', { day: day(i.created_at) })}</span>
          <span className="text-[12.5px] text-t3">{tr('до {when}', { when: when(i.expires_at) })}</span>
          <ConfirmButton className="shrink-0 p-0 text-[12.5px] text-t3 hover:text-rd" label={tr('Отозвать')} ariaLabel={tr('Отозвать приглашение от {day}', { day: day(i.created_at) })} armed={tr('Отозвать приглашение?')} disabled={revoke.isPending} onConfirm={() => revoke.mutate(i.id)} />
        </div>
      ))}
      {revoke.isError && <div className="text-rd text-[13px]" role="alert">{errText(revoke.error)}</div>}
      {pending.isPending && <span className="px-3 text-[12.5px] text-t3">{tr('Загрузка…')}</span>}
      {pending.data?.length === 0 && <span className="px-3 text-[12.5px] text-t3">{tr('Активных приглашений нет')}</span>}
    </div>
  )
}

export function Users({ me }: { me: AuthUser }) {
  const qc = useQueryClient()
  const users = useQuery({ queryKey: ['admin', 'users'], queryFn: api.adminUsers })
  const registration = useQuery({ queryKey: ['signup-mode'], queryFn: api.registration })
  const mode = useMutation({ mutationFn: api.setRegistration, onSuccess: () => qc.invalidateQueries({ queryKey: ['signup-mode'] }) })
  const block = useMutation({
    mutationFn: ({ id, blocked }: { id: number; blocked: boolean }) => api.blockUser(id, blocked),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'users'] }),
  })
  return (
    <>
      <div className="flex flex-col gap-1.5">
        <div className="grid gap-1"><span className={cap}>{tr('Регистрация')}</span>
          <Select<Registration> block label={tr('Регистрация')} value={registration.data?.mode ?? 'invite'} disabled={!registration.data || mode.isPending} onChange={(v) => mode.mutate(v)}
            options={[{ value: 'invite', label: tr('По приглашению') }, { value: 'closed', label: tr('Закрытая') }, { value: 'open', label: tr('Открытая') }]} /></div>
        {(mode.isError || registration.isError) && <p role="alert">{errText(mode.error ?? registration.error)}</p>}
        <span className={cap}>{tr('Пользователи')}</span>
        {users.isPending && <span className="px-3 text-[12.5px] text-t3">{tr('Загрузка…')}</span>}
        {users.data?.map((u) => (
          <div key={u.id} className={item}>
            <span className="grow min-w-0 truncate">{u.username}{u.id === me.id && <span className="ml-2 text-xs text-t3">{tr('вы')}</span>}</span>
            <span className="shrink-0 text-[12.5px] text-t3">{day(u.created_at)}</span>
            {u.id !== me.id && (
              <ConfirmButton key={String(!!u.blocked)} className="shrink-0 max-w-[65%] p-0 text-right text-[12.5px] text-t3 hover:text-rd" label={u.blocked ? tr('Разблокировать') : tr('Заблокировать')} ariaLabel={u.blocked ? tr('Разблокировать {name}', { name: u.username }) : tr('Заблокировать {name}', { name: u.username })} armed={tr('Подтвердить')} disabled={block.isPending} onConfirm={() => block.mutate({ id: u.id, blocked: !u.blocked })} />
            )}
          </div>
        ))}
        {users.isError && <div className="text-rd text-[13px]" role="alert">{errText(users.error)}</div>}
        {block.isError && <div className="text-rd text-[13px]" role="alert">{errText(block.error)}</div>}
      </div>
      <Invites closed={registration.data?.mode === 'closed'} />
    </>
  )
}

function OwnerControls({ admin }: { admin: boolean }) {
  const [action, setAction] = useState<'code' | 'delete' | null>(null)
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const confirm = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      if (action === 'delete') await api.deleteAccount(password)
      else setCode((await api.issueRecovery(password)).recovery_code)
    }, onSuccess: () => { setAction(null); setPassword('') },
  })
  const select = (value: typeof action) => { confirm.reset(); setCode(''); setPassword(''); setAction(value) }
  return <div className="grid gap-2">
    <div className="flex gap-2 flex-wrap"><button className={btn} onClick={() => select('code')}>{tr('Новый резервный код')}</button>
      {!admin && <button className={btn} onClick={() => select('delete')}>{tr('Удалить мой аккаунт')}</button>}</div>
    {action && <form className="grid gap-2" onSubmit={e => { e.preventDefault(); if (!confirm.isPending) confirm.mutate() }}>
      <p className="text-sm">{action === 'delete' ? tr('Рабочие данные будут удалены. Для подтверждения введите пароль.') : tr('Предыдущий код перестанет действовать. Введите пароль.')}</p>
      <input className={input} aria-label={tr('Текущий пароль')} type="password" autoComplete="current-password" required value={password} onChange={e => setPassword(e.target.value)} />
      <div className="flex gap-2"><button className={btnPri} disabled={confirm.isPending}>{tr('Подтвердить')}</button><button type="button" className={btn} disabled={confirm.isPending} onClick={() => select(null)}>{tr('Отмена')}</button></div>
      {confirm.isError && <p role="alert">{errText(confirm.error)}</p>}
    </form>}
    {code && <div role="status" className="grid gap-2"><b>{tr('Сохраните резервный код')}</b><code className="select-all break-all">{code}</code><button className={btn} onClick={() => setCode('')}>{tr('Код сохранён')}</button></div>}
  </div>
}

export function AccountSection({ user }: { user: AuthUser }) {
  const logout = useMutation({ mutationFn: api.logout }) // api.logout itself sends the user back to the login screen
  return (
    <>
      <div className="flex flex-col gap-1.5">
        <span className={cap}>{tr('Аккаунт')}</span>
        <div className={item}>
          <span className="grow min-w-0 truncate">{user.username}{user.is_admin && <span className="ml-2 text-xs text-t3">{tr('администратор')}</span>}</span>
          <button type="button" className={btn} disabled={logout.isPending} onClick={() => logout.mutate()}>{tr('Выйти')}</button>
        </div>
        {logout.isError && <div className="text-rd text-[13px]" role="alert">{errText(logout.error)}</div>}
      </div>
      <PasskeySettings />
      <ChangePassword username={user.username} /><OwnerControls admin={user.is_admin} />
    </>
  )
}
