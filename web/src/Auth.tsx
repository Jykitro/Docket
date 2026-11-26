// Self-host gate: asks the server who we are before <App /> mounts (so no app hook or SSE runs before login) and
// shows setup, login or invite screens. In local mode, or with a session, it renders the app untouched.
import { type ChangeEvent, type ReactNode, useEffect, useId, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, api, errText, errorKind, useMe } from './api'
import { Icon } from './icons'
import { PasskeyLogin } from './Passkeys'
import { PlandilloLogo } from './PlandilloLogo'
import { tr, useLang } from './i18n'
import { Link, navigate, useRoute } from './router'
import { ErrorPage, LangSwitch, btn, btnPri, copyInput } from './ui'

const GITHUB = 'https://github.com/huntly-digital/plandillo'
const input = 'h-11 w-full min-w-0 rounded-md border border-ln2 bg-sf px-3.5 text-[15px] text-tx focus:border-ring focus:ring-[3px] focus:ring-ring/30 focus:outline-none aria-invalid:border-rd'
const bigPri = 'inline-flex h-11 w-full items-center justify-center gap-2 rounded-md border border-ink bg-ink text-[15px] font-semibold text-inkt shadow-xs hover:bg-ink/90 disabled:opacity-50'
const link = 'font-medium text-act hover:underline'
const note = 'text-[13px] text-t3'

// Decorative activity on the brand panel: fixed example rows, never real data.
const FEED = [
  { who: 'Claude', key: 'PLN-12', title: 'Настроить ежедневный бэкап', action: 'перевёл в «Готово»', time: 'сейчас' },
  { who: 'Codex', key: 'PLN-15', title: 'Проверить вход по ключу доступа', action: 'добавил заметку', time: '2 мин' },
  { who: 'Claude', key: 'Вики', title: 'Архитектура сервера', action: 'обновил страницу', time: '5 мин' },
]

function BrandPanel() {
  return (
    <aside aria-label={tr('О Plandillo')} className="relative m-3 hidden min-w-0 flex-[1_1_640px] flex-col justify-between gap-10 overflow-hidden rounded-xl bg-[#3b2272] p-14 text-white lg:flex">
      <svg aria-hidden="true" viewBox="0 0 800 800" preserveAspectRatio="xMaxYMax slice" className="pointer-events-none absolute inset-0 size-full">
        <g fill="none" stroke="#cdb7ff" strokeOpacity="0.13" strokeWidth="22">
          {[180, 260, 340, 420, 500, 580, 660].map((r) => <circle key={r} cx="820" cy="900" r={r} />)}
        </g>
      </svg>
      <div className="relative flex max-w-[480px] flex-col gap-4">
        <h2 className="text-4xl leading-[1.12] font-semibold tracking-[-0.025em]">{tr('Задачи, которые ведёте вы и ваши агенты')}</h2>
        <p className="text-base leading-relaxed text-white/80">{tr('Claude Code и Codex читают и обновляют задачи через MCP. Вики проекта хранит контекст, а данные остаются на вашем сервере.')}</p>
      </div>
      <div aria-hidden="true" className="relative flex flex-col gap-3">
        {FEED.map((f, i) => (
          <div key={f.key} className="flex w-[380px] max-w-full items-center gap-3 rounded-xl bg-white px-3.5 py-3 text-[#1b1530] shadow-[0_12px_32px_rgba(10,4,30,0.35)]" style={{ marginLeft: i * 48 }}>
            <span className="grid size-[30px] shrink-0 place-items-center rounded-md bg-[#ece6fa] font-mono text-[11px] font-semibold text-[#3b2272]">{f.who.slice(0, 2)}</span>
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-[13.5px] font-semibold"><span className="font-mono text-xs font-medium text-[#6b6480]">{tr(f.key)}</span> {tr(f.title)}</span>
              <span className="text-[12.5px] text-[#5d5672]">{f.who} {tr(f.action)}</span>
            </span>
            <span className="text-xs whitespace-nowrap text-[#6b6480]">{tr(f.time)}</span>
          </div>
        ))}
      </div>
      <p className="relative text-[13px] text-white/70">{tr('Открытый код. Работает на вашем сервере, одним контейнером.')}</p>
    </aside>
  )
}

// Every guest screen: the form column on the left, the brand panel on wide screens.
function AuthShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-screen">
      <main className="flex min-w-0 flex-[1_1_520px] flex-col gap-6 px-5 pt-5 pb-6 sm:px-10 sm:py-7">
        <div className="flex items-center justify-between gap-3"><PlandilloLogo /><LangSwitch /></div>
        <div className="flex flex-1 items-center justify-center">
          <div className="flex w-full max-w-[360px] flex-col gap-6">{children}</div>
        </div>
        <a href={GITHUB} target="_blank" rel="noopener noreferrer" className="self-center text-[12.5px] text-t3 hover:text-t2">{tr('Plandillo — открытый код на GitHub')}</a>
      </main>
      <BrandPanel />
    </div>
  )
}

function Heading({ head, title, text }: { head?: ReactNode; title: string; text: string }) {
  return (
    <header className="flex flex-col items-start gap-3">
      {head}
      <div className="flex flex-col gap-1.5">
        <h1 className="text-[28px] leading-[1.15] font-semibold tracking-[-0.02em]">{title}</h1>
        <p className="text-t2">{text}</p>
      </div>
    </header>
  )
}

/** Which server the guest is signing in to; an invite gets the accent colour. */
function ServerChip({ invite }: { invite?: boolean }) {
  return (
    <span title={tr('Сервер, на который вы входите')} className={'inline-flex h-[26px] items-center gap-1.5 rounded-full px-2.5 font-mono text-xs ' + (invite ? 'bg-acs text-act' : 'border border-ln text-t2')}>
      <Icon name={invite ? 'mail' : 'stack'} size={13} sw={2} />{location.host}
    </span>
  )
}

const Tile = ({ icon, ok }: { icon: string; ok?: boolean }) => (
  <span aria-hidden="true" className={'grid size-11 place-items-center rounded-lg ' + (ok ? 'bg-dns text-dnt' : 'bg-acs text-act')}><Icon name={icon} size={20} sw={1.9} /></span>
)

type FieldDef = { key: string; label: string; autoComplete: string; password?: boolean; kind?: 'otp' | 'code'; aside?: ReactNode }

const LEN_STEPS = [3, 6, 10, 16] // the third bar is the 10-character minimum, the fourth a bonus

function Field({ f, value, first, invalid, onChange }: { f: FieldDef; value: string; first: boolean; invalid: boolean; onChange: (v: string) => void }) {
  const id = useId()
  const [show, setShow] = useState(false)
  const fresh = f.autoComplete === 'new-password'
  const hint = fresh || f.kind === 'otp' ? id + 'h' : undefined
  const common = {
    id, required: true, autoFocus: first, autoComplete: f.autoComplete, value, 'aria-describedby': hint,
    onChange: (e: ChangeEvent<HTMLInputElement>) => onChange(e.target.value),
  }
  let control: ReactNode
  if (f.kind === 'otp') {
    // One real input (paste and one-time-code autofill keep working) drawn as six cells.
    control = (
      <div className="group relative">
        <input {...common} className="absolute inset-0 z-10 size-full cursor-text text-base opacity-0" inputMode="numeric" pattern="\d{6}"
          onChange={(e) => onChange(e.target.value.replace(/\D/g, '').slice(0, 6))} />
        <div aria-hidden="true" className="grid grid-cols-6 gap-2">
          {Array.from({ length: 6 }, (_, i) => (
            <span key={i} className={'grid h-[52px] place-items-center rounded-md border border-ln2 bg-sf font-mono text-[22px] font-medium '
              + (i === Math.min(value.length, 5) ? 'group-focus-within:border-ring group-focus-within:ring-[3px] group-focus-within:ring-ring/30' : '')}>{value[i]}</span>
          ))}
        </div>
      </div>
    )
  } else if (f.password) {
    control = (
      <div className="relative">
        <input {...common} className={input + ' pr-11'} type={show ? 'text' : 'password'} aria-invalid={invalid || undefined}
          minLength={fresh ? 10 : undefined} maxLength={128} />
        <button type="button" className="absolute inset-y-0 right-0 grid w-11 place-items-center rounded-r-md text-t3 hover:text-tx" aria-pressed={show}
          aria-label={show ? tr('Скрыть пароль') : tr('Показать пароль')} onClick={() => setShow(!show)}>
          <Icon name={show ? 'eyeOff' : 'eye'} size={17} sw={1.8} />
        </button>
      </div>
    )
  } else {
    control = <input {...common} type="text" spellCheck={f.kind === 'code' ? false : undefined} className={input + (f.kind === 'code' ? ' font-mono text-sm tracking-[0.04em]' : '')} />
  }
  const ok = value.length >= 10
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <label htmlFor={id} className={f.kind === 'otp' ? 'sr-only' : 'text-[13px] font-medium text-t2'}>{tr(f.label)}</label>
        {f.aside}
      </div>
      {control}
      {fresh && (
        <div className="mt-0.5 flex flex-col gap-1.5">
          <div aria-hidden="true" className="grid grid-cols-4 gap-1">
            {LEN_STEPS.map((n) => <span key={n} className={'h-1 rounded-full ' + (value.length < n ? 'bg-sk' : ok ? 'bg-dn' : 'bg-dg')} />)}
          </div>
          <span id={hint} className={'flex items-center gap-1.5 text-[12.5px] ' + (ok ? 'text-dnt' : 'text-t3')}>{ok && <Icon name="check" size={13} sw={3} />}{tr('Не короче 10 символов.')}</span>
        </div>
      )}
      {f.kind === 'otp' && <span id={hint} className="text-[12.5px] text-t3">{tr('Код меняется каждые 30 секунд. Вставка из буфера работает.')}</span>}
    </div>
  )
}

// One layout for every form: heading, optional block above the fields, the server's error, the fields and a full-width submit.
function AuthForm({ title, text, head, top, fields, submit, onSubmit, footer, wrongPassword }: {
  title: string; text: string; head?: ReactNode; top?: ReactNode; fields: FieldDef[]; submit: string
  onSubmit: (v: Record<string, string>) => Promise<unknown>; footer?: ReactNode; wrongPassword?: boolean
}) {
  const qc = useQueryClient()
  const [values, setValues] = useState<Record<string, string>>({})
  const send = useMutation({
    gcTime: 0,
    mutationFn: () => onSubmit(values),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['me'] }),
  })
  const message = send.error && errText(send.error)
  // The layout hint only helps when the password was rejected, not for a limit or a network error.
  const badPassword = !!wrongPassword && send.error instanceof ApiError && send.error.code === 'invalid_credentials'
  return (
    <AuthShell>
      <Heading head={head} title={title} text={text} />
      {top}
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); if (!send.isPending) send.mutate() }}>
        {message && (
          <div role="alert" className="flex items-start gap-2.5 rounded-md bg-rds px-3 py-2.5 text-[13px] text-rd">
            <span className="mt-px shrink-0"><Icon name="alert" size={16} sw={2} /></span>
            <span>{badPassword ? `${message}. ${tr('Проверьте раскладку и Caps Lock.')}` : message}</span>
          </div>
        )}
        {fields.map((f, i) => (
          <Field key={f.key} f={f} first={i === 0} value={values[f.key] ?? ''} invalid={badPassword && !!f.password}
            onChange={(v) => setValues({ ...values, [f.key]: v })} />
        ))}
        <button type="submit" className={bigPri + ' mt-1'} disabled={send.isPending}>{submit}</button>
      </form>
      {footer}
    </AuthShell>
  )
}

const username: FieldDef = { key: 'username', label: 'Имя пользователя', autoComplete: 'username' }
const newPassword: FieldDef = { key: 'password', label: 'Пароль', password: true, autoComplete: 'new-password' }
const backToLogin = (onClick?: () => void) => onClick
  ? <button type="button" className="self-start text-[13px] text-t2 hover:text-tx" onClick={onClick}>{tr('Назад ко входу')}</button>
  : <Link className="self-start text-[13px] text-t2 hover:text-tx" to="/">{tr('Назад ко входу')}</Link>

function Login({ open, passkeys }: { open: boolean; passkeys: boolean }) {
  const [challenge, setChallenge] = useState<{ id: string; user: string } | null>(null)
  if (challenge) return (
    <AuthForm key="totp" head={<Tile icon="phone" />} title={tr('Код из приложения')} text={tr('Введите шесть цифр из аутентификатора для {name}.', { name: challenge.user })}
      submit={tr('Подтвердить')} fields={[{ key: 'code', label: 'Код', autoComplete: 'one-time-code', kind: 'otp' }]}
      onSubmit={(v) => api.finishTOTP(challenge.id, v.code)} footer={backToLogin(() => setChallenge(null))} />
  )
  return (
    <AuthForm
      head={<ServerChip />} title={tr('Вход в Plandillo')} text={tr('Войдите, чтобы открыть свои проекты.')} submit={tr('Войти')} wrongPassword
      top={passkeys && <PasskeyLogin />}
      fields={[username, { key: 'password', label: 'Пароль', password: true, autoComplete: 'current-password', aside: <Link className={'text-[13px] ' + link} to="/recover">{tr('Забыли пароль?')}</Link> }]}
      onSubmit={async (v) => { const result = await api.login(v.username, v.password); if ('challenge' in result) setChallenge({ id: result.challenge, user: v.username }) }}
      footer={<p className={note}>{open
        ? <>{tr('Нет аккаунта?')} <Link className={link} to="/register">{tr('Создать аккаунт')}</Link></>
        : tr('Нет аккаунта? Попросите приглашение у администратора сервера.')}</p>}
    />
  )
}

type IssuedCode = { id?: number; recovery_code: string }
type Saved = (response: IssuedCode) => void

function Setup({ saved }: { saved: Saved }) {
  // The first account becomes the administrator.
  return (
    <AuthForm
      head={<ServerChip />} title={tr('Создайте администратора')} text={tr('Этот аккаунт управляет сервером и приглашает других пользователей.')} submit={tr('Создать администратора')}
      fields={[username, newPassword]} onSubmit={(v) => api.setup(v.username, v.password).then(saved)}
      footer={<p className={note}>{tr('После создания сервер покажет резервный код. Он нужен, если забудете пароль.')}</p>}
    />
  )
}

function Guest({ path, saved }: { path: string; saved: Saved }) {
  const policy = useQuery({ queryKey: ['signup-mode'], queryFn: api.registration, retry: 1 })
  const toLogin = <p className={note}>{tr('Уже есть аккаунт?')} <Link className={link} to="/">{tr('Войти')}</Link></p>
  if (path === '/recover') return (
    <AuthForm title={tr('Восстановление доступа')} text={tr('Введите резервный код и задайте новый пароль. Ключи доступа после этого нужно добавить заново.')} submit={tr('Восстановить доступ')}
      fields={[username, { key: 'code', label: 'Резервный код', autoComplete: 'off', kind: 'code' }, { ...newPassword, label: 'Новый пароль' }]}
      onSubmit={(v) => api.recover(v.username, v.code, v.password).then(saved)} footer={backToLogin()} />
  )
  const invite = path.startsWith('/invite/')
  if (invite || path === '/register') {
    if (policy.isPending) return null
    if (policy.data?.mode === 'open' || (invite && policy.data?.mode === 'invite')) {
      return (
        <AuthForm head={<ServerChip invite={invite} />} title={invite ? tr('Вас пригласили в Plandillo') : tr('Создать аккаунт')}
          text={invite ? tr('Придумайте логин и пароль. После регистрации откроется ваше личное рабочее пространство.') : tr('После регистрации откроется ваше личное рабочее пространство.')}
          submit={tr('Создать аккаунт')} fields={[username, newPassword]} footer={toLogin}
          onSubmit={(v) => (invite ? api.acceptInvite(path.slice(8), v.username, v.password) : api.register(v.username, v.password)).then(saved)} />
      )
    }
    return <AuthShell><Heading title={tr('Регистрация сейчас недоступна')} text={tr('Нет аккаунта? Попросите приглашение у администратора сервера.')} />{backToLogin()}</AuthShell>
  }
  return <Login open={policy.data?.mode === 'open'} passkeys={policy.data?.passkeys === true} />
}

function SaveCode({ code, onDone }: { code: string; onDone: () => void }) {
  const field = useRef<HTMLTextAreaElement>(null)
  const [copied, setCopied] = useState<'done' | 'manual' | null>(null)
  const download = () => {
    const url = URL.createObjectURL(new Blob([code], { type: 'text/plain' }))
    const a = document.createElement('a')
    a.href = url
    a.download = 'plandillo-recovery-code.txt'
    a.click()
    setTimeout(() => URL.revokeObjectURL(url))
  }
  return (
    <AuthShell>
      <Heading head={<Tile icon="shield" ok />} title={tr('Сохраните резервный код')} text={tr('Одноразовый код позволит задать новый пароль. Показываем его только сейчас: сохраните в менеджере паролей.')} />
      <div className="flex flex-col gap-2.5 rounded-lg border border-dashed border-ln2 bg-sf2 p-4">
        <textarea ref={field} readOnly rows={2} value={code} aria-label={tr('Резервный код')} onFocus={(e) => e.currentTarget.select()}
          className="w-full resize-none bg-transparent text-center font-mono text-[15px] font-medium break-all text-tx" />
        <div className="flex gap-2">
          <button type="button" className={btn + ' flex-1 justify-center'} onClick={async () => setCopied(await copyInput(field.current))}>
            <Icon name="copy" size={14} sw={2} />{copied === 'done' ? tr('Скопировано') : tr('Копировать')}
          </button>
          <button type="button" className={btn + ' flex-1 justify-center'} onClick={download}><Icon name="download" size={14} sw={2} />{tr('Скачать .txt')}</button>
        </div>
        {copied === 'manual' && <span className="text-xs text-t2" role="status">{tr('Выделено — нажмите Ctrl+C')}</span>}
      </div>
      <button type="button" className={bigPri} onClick={onDone}>{tr('Код сохранён, продолжить')}</button>
    </AuthShell>
  )
}

export function Gate({ children }: { children: ReactNode }) {
  const me = useMe()
  const [returnPath] = useState(() => location.pathname === '/oauth/authorize' ? location.pathname + location.search : '/')
  const [issued, setIssued] = useState<IssuedCode | null>(null)
  const { path } = useRoute()
  useLang() // re-render the login screens when the language changes (App does the same for the app)

  const { refetch } = me
  useEffect(() => {
    const verify = () => { void refetch() }
    window.addEventListener('focus', verify)
    window.addEventListener('pageshow', verify)
    return () => { window.removeEventListener('focus', verify); window.removeEventListener('pageshow', verify) }
  }, [refetch])
  // Offline at start the refetch fails but the restored data is still good: treat it as loaded.
  const loaded = me.isSuccess || (!!me.data && errorKind(me.error) === 'offline')
  const loggedOut = loaded && me.data?.mode === 'selfhost' && !me.data.user && !me.data.setup

  // App sets page-specific titles that would go stale behind the login screen.
  const inApp = loaded && (me.data?.mode === 'local' || !!me.data?.user)
  useEffect(() => {
    if (!inApp) document.title = 'Plandillo'
  }, [inApp])

  // Signed in already: drop /invite/<token> from the URL and history instead of letting the app print it as "not found".
  const leaving = !loggedOut && !!me.data?.user && (path.startsWith('/invite/') || path === '/register' || path === '/recover')
  useEffect(() => {
    if (leaving) navigate(returnPath, true)
  }, [leaving, returnPath])

  const wrap = (el: ReactNode) => <div className="min-h-screen bg-bg text-tx">{el}</div>
  const saved: Saved = ({ recovery_code, id }) => { setIssued({ recovery_code, id }); navigate(returnPath, true) }
  // Registration signs in the new owner, while recovery issues a code on the guest screen.
  if (issued && (issued.id === undefined ? loggedOut : me.data?.user?.id === issued.id)) return wrap(<SaveCode code={issued.recovery_code} onDone={() => setIssued(null)} />)
  if (me.isPending) return <div className="min-h-screen bg-bg" /> // no flash of the app
  if (loggedOut) {
    return wrap(<Guest key={path} path={path} saved={saved} />)
  }
  if (leaving) return null
  if (inApp) return <>{children}</>
  if (me.data?.setup) return wrap(<Setup saved={saved} />)
  return <ErrorPage full kind={me.error ? undefined : 'offline'} error={me.error}
    actions={<button type="button" className={btnPri} onClick={() => me.refetch()}>{tr('Повторить')}</button>} />
}
