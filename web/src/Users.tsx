// Self-host account blocks of the settings pages: sign-in and password (Security) and, for admins, users and invites
// (registration mode, one table of accounts and pending invites, and the invite side panel).
import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, type AuthUser, type Registration, api, errText } from './api'
import { PasskeySettings } from './Passkeys'
import { Icon } from './icons'
import { locale, tr } from './i18n'
import { Avatar, ConfirmButton, Dialog, ErrorState, SettingsSection, SkeletonRows, btn, btnPri, cap, copyInput } from './ui'

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

/** The "Invite" side panel: explain, create a link, show it once with a copy button. */
function InvitePanel({ closed, onClose }: { closed: boolean; onClose: () => void }) {
  const qc = useQueryClient()
  const [copied, setCopied] = useState<'done' | 'manual' | null>(null)
  const create = useMutation({
    mutationFn: api.createInvite,
    onSuccess: () => { setCopied(null); return qc.invalidateQueries({ queryKey: ['admin', 'invites'] }) }, // a new link drops the old copy state
  })
  const link = create.data && location.origin + create.data.url
  const field = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (!copied) return
    const t = setTimeout(() => setCopied(null), copied === 'done' ? 2000 : 4000)
    return () => clearTimeout(t)
  }, [copied])
  const copy = async () => setCopied(await copyInput(field.current))
  return (
    <Dialog side title={tr('Пригласить пользователя')} onClose={onClose}>
      <p className="text-sm text-t2">{tr('Создайте ссылку и отправьте её человеку. По ней он создаст аккаунт на этом сервере. Ссылка одноразовая.')}</p>
      {closed && <p className="rounded-lg bg-sk p-3 text-sm text-t2">{tr('Регистрация закрыта: приглашения не работают. Переключите регистрацию на «По приглашению».')}</p>}
      <div><button type="button" className={btnPri} disabled={closed || create.isPending} onClick={() => create.mutate()}>{link ? tr('Создать ещё одну') : tr('Создать ссылку')}</button></div>
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
      <span className="text-xs text-t3">{tr('Новое приглашение не отменяет прежние.')}</span>
      <div className="mt-auto flex justify-end"><button type="button" className={btn} onClick={onClose}>{tr('Закрыть')}</button></div>
    </Dialog>
  )
}

const REG: { id: Registration; label: string; hint: string }[] = [
  { id: 'invite', label: 'По приглашению', hint: 'Новые пользователи входят только по ссылке-приглашению от администратора.' },
  { id: 'open', label: 'Открытая', hint: 'Любой, кто знает адрес сервера, может создать аккаунт.' },
  { id: 'closed', label: 'Закрытая', hint: 'Новые аккаунты не создаются, приглашения не работают.' },
]
type Filter = 'all' | 'active' | 'blocked' | 'invites'
const TH = 'h-9 px-3 text-left text-xs font-medium text-t3 first:pl-5 last:pr-5'
const TD = 'px-3 py-2.5 align-middle first:pl-5 last:pr-5'
const badge = (ok: boolean) => 'inline-flex rounded-full px-2 py-0.5 text-xs font-medium ' + (ok ? 'bg-dns text-dnt' : 'bg-rds text-rd')

export function Users({ me, inviting, onInvited }: { me: AuthUser; inviting: boolean; onInvited: () => void }) {
  const qc = useQueryClient()
  const users = useQuery({ queryKey: ['admin', 'users'], queryFn: api.adminUsers })
  const invites = useQuery({ queryKey: ['admin', 'invites'], queryFn: api.adminInvites })
  const registration = useQuery({ queryKey: ['signup-mode'], queryFn: api.registration })
  const mode = useMutation({ mutationFn: api.setRegistration, onSuccess: () => qc.invalidateQueries({ queryKey: ['signup-mode'] }) })
  const block = useMutation({
    mutationFn: ({ id, blocked }: { id: number; blocked: boolean }) => api.blockUser(id, blocked),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'users'] }),
  })
  const revoke = useMutation({
    // 404: already used, expired or revoked elsewhere; the refreshed list shows it.
    mutationFn: (id: number) => api.revokeInvite(id).catch((e) => { if (!(e instanceof ApiError && e.code === 'not_found')) throw e }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['admin', 'invites'] }),
  })
  const [filter, setFilter] = useState<Filter>('all')
  const [q, setQ] = useState('')
  const reg = registration.data?.mode ?? 'invite'
  const all = users.data ?? []
  const pending = invites.data ?? []
  const counts: Record<Filter, number> = { all: all.length + pending.length, active: all.filter((u) => !u.blocked).length, blocked: all.filter((u) => u.blocked).length, invites: pending.length }
  const needle = q.trim().toLowerCase()
  const shownUsers = filter === 'invites' ? [] : all.filter((u) => (filter === 'all' || (filter === 'blocked') === !!u.blocked)
    && (!needle || (u.username + ' ' + u.name).toLowerCase().includes(needle)))
  const shownInvites = (filter === 'all' || filter === 'invites') && !needle ? pending : []
  const error = users.error ?? invites.error
  return (
    <>
      <SettingsSection title={tr('Регистрация')}>
        <div className="flex flex-col gap-2 px-5 py-4">
          <span className="inline-flex w-fit flex-wrap gap-0.5 rounded-md border border-ln p-0.5 text-[13px]" role="radiogroup" aria-label={tr('Регистрация')}>
            {REG.map((r) => (
              <button key={r.id} type="button" role="radio" aria-checked={reg === r.id} disabled={!registration.data || mode.isPending} onClick={() => mode.mutate(r.id)}
                className="rounded-sm px-3 py-1 text-t3 aria-checked:bg-sk aria-checked:font-semibold aria-checked:text-tx disabled:opacity-60">{tr(r.label)}</button>
            ))}
          </span>
          <p className="text-[13px] text-t2">{tr(REG.find((r) => r.id === reg)!.hint)}</p>
          {(mode.isError || registration.isError) && <p className="text-[13px] text-rd" role="alert">{errText(mode.error ?? registration.error)}</p>}
        </div>
      </SettingsSection>

      <SettingsSection title={tr('Учётные записи')}>
        <div className="flex flex-wrap items-center gap-2 px-5 py-3">
          <span className="flex flex-wrap gap-1.5" role="radiogroup" aria-label={tr('Фильтр')}>
            {([['all', 'Все'], ['active', 'Активные'], ['blocked', 'Заблокированные'], ['invites', 'Приглашения']] as const).map(([k, n]) => (
              <button key={k} type="button" role="radio" aria-checked={filter === k} onClick={() => setFilter(k)}
                className="inline-flex h-7 items-center gap-1.5 rounded-full border border-ln px-2.5 text-[12.5px] text-t2 hover:bg-sk aria-checked:border-transparent aria-checked:bg-acs aria-checked:font-medium aria-checked:text-act">
                {tr(n)}<span className="tabular-nums text-t3">{counts[k]}</span>
              </button>
            ))}
          </span>
          <label className="ml-auto flex h-8 min-w-0 flex-[0_1_220px] items-center gap-2 rounded-md border border-ln bg-sf px-2.5 text-t3">
            <Icon name="search" size={14} />
            <input className="w-full border-0 bg-transparent text-[13px] text-tx outline-0" type="search" placeholder={tr('Найти по имени или логину')} aria-label={tr('Найти по имени или логину')} value={q} onChange={(e) => setQ(e.target.value)} />
          </label>
        </div>
        {error && <div className="border-t border-ln"><ErrorState error={error} onRetry={() => { users.refetch(); invites.refetch() }} /></div>}
        {(users.isPending || invites.isPending) && <div className="border-t border-ln"><SkeletonRows n={3} /></div>}
        {users.isSuccess && invites.isSuccess && (
          <div className="overflow-x-auto border-t border-ln">
            <table className="w-full min-w-[560px] text-sm">
              <thead><tr className="bg-sf2"><th scope="col" className={TH}>{tr('Пользователь')}</th><th scope="col" className={TH}>{tr('Роль')}</th><th scope="col" className={TH}>{tr('Создан')}</th><th scope="col" className={TH}>{tr('Статус')}</th><th scope="col" className={TH}><span className="sr-only">{tr('Действие')}</span></th></tr></thead>
              <tbody>
                {shownUsers.map((u) => (
                  <tr key={'u' + u.id} className="border-t border-ln">
                    <td className={TD}><span className="flex min-w-0 items-center gap-2.5">
                      <Avatar name={u.name || u.username} photo={u.photo ? u.photo + '?v=' + u.updated_at : null} size={28} />
                      <span className="flex min-w-0 flex-col leading-tight"><span className="truncate font-medium">{u.name || u.username}{u.id === me.id && <span className="ml-1.5 text-xs font-normal text-t3">{tr('вы')}</span>}</span>
                        <span className="truncate font-mono text-xs text-t3">{u.username}</span></span>
                    </span></td>
                    <td className={TD + ' whitespace-nowrap text-t2'}>{u.is_admin ? tr('Администратор') : tr('Пользователь')}</td>
                    <td className={TD + ' whitespace-nowrap text-t2 tabular-nums'}>{day(u.created_at)}</td>
                    <td className={TD}><span className={badge(!u.blocked)}>{u.blocked ? tr('Заблокирован') : tr('Активен')}</span></td>
                    <td className={TD + ' text-right'}>{u.id !== me.id && (
                      <ConfirmButton key={String(!!u.blocked)} className="p-0 text-[12.5px] text-t3 hover:text-rd [&.warn]:text-rd" label={u.blocked ? tr('Разблокировать') : tr('Заблокировать')} ariaLabel={u.blocked ? tr('Разблокировать {name}', { name: u.username }) : tr('Заблокировать {name}', { name: u.username })} armed={tr('Подтвердить')} disabled={block.isPending} onConfirm={() => block.mutate({ id: u.id, blocked: !u.blocked })} />
                    )}</td>
                  </tr>
                ))}
                {shownInvites.map((i) => (
                  <tr key={'i' + i.id} className="border-t border-ln">
                    <td className={TD}><span className="flex items-center gap-2.5">
                      <span className="grid size-7 shrink-0 place-items-center rounded-full border border-dashed border-ln2 text-t3"><Icon name="link" size={13} /></span>
                      <span className="flex flex-col leading-tight"><span className="font-medium">{tr('Приглашение')}</span><span className="text-xs text-t3">{tr('до {when}', { when: when(i.expires_at) })}</span></span>
                    </span></td>
                    <td className={TD + ' text-t3'}>—</td>
                    <td className={TD + ' whitespace-nowrap text-t2 tabular-nums'}>{day(i.created_at)}</td>
                    <td className={TD}><span className="inline-flex rounded-full bg-sk px-2 py-0.5 text-xs font-medium text-t2">{tr('Ожидает')}</span></td>
                    <td className={TD + ' text-right'}><ConfirmButton className="p-0 text-[12.5px] text-t3 hover:text-rd [&.warn]:text-rd" label={tr('Отозвать')} ariaLabel={tr('Отозвать приглашение от {day}', { day: day(i.created_at) })} armed={tr('Отозвать приглашение?')} disabled={revoke.isPending} onConfirm={() => revoke.mutate(i.id)} /></td>
                  </tr>
                ))}
                {!shownUsers.length && !shownInvites.length && <tr className="border-t border-ln"><td colSpan={5} className={TD + ' text-center text-t3'}>{filter === 'invites' ? tr('Активных приглашений нет') : tr('Никого не найдено')}</td></tr>}
              </tbody>
            </table>
          </div>
        )}
        {block.isError && <p className="border-t border-ln px-5 py-3 text-[13px] text-rd" role="alert">{errText(block.error)}</p>}
        {revoke.isError && <p className="border-t border-ln px-5 py-3 text-[13px] text-rd" role="alert">{errText(revoke.error)}</p>}
      </SettingsSection>
      <p className="text-[13px] text-t3">{tr('Агенты (Claude Code, Codex) в списке не появляются: они действуют от имени пользователя, который их подключил, и видны в его журнале вызовов.')}</p>
      {inviting && <InvitePanel closed={reg === 'closed'} onClose={onInvited} />}
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
