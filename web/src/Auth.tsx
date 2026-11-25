// Self-host gate: asks the server who we are before <App /> mounts (so no app hook or SSE runs before login) and
// shows setup, login or invite screens. In local mode, or with a session, it renders the app untouched.
import { type ReactNode, useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errText, useMe } from './api'
import { Icon } from './icons'
import { PasskeyLogin } from './Passkeys'
import { PlandilloLogo } from './PlandilloLogo'
import { tr, useLang } from './i18n'
import { Link, navigate, useRoute } from './router'
import { LangSwitch, btnPri, cap } from './ui'

const inputCls = 'h-[38px] border border-ln rounded-md bg-sf px-3 text-[14px] text-tx focus:border-ring focus:ring-[3px] focus:ring-ring focus:outline-none'

type FieldDef = { key: string; label: string; password?: boolean; autoComplete: string; hint?: string }

// One card for all three screens: the fields, a submit button and the server's error.
function AuthForm({ title, text, fields, submit, onSubmit, children }: {
  children?: ReactNode; title: string; text: string; fields: FieldDef[]; submit: string; onSubmit: (v: Record<string, string>) => Promise<unknown>
}) {
  const qc = useQueryClient()
  const [values, setValues] = useState<Record<string, string>>({})
  const send = useMutation({
    gcTime: 0,
    mutationFn: () => onSubmit(values),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['me'] }),
  })
  const message = send.error && errText(send.error)
  return (
    <main className="min-h-screen grid grid-cols-1 place-items-center px-4 py-8">
      <div className="w-full max-w-[460px] bg-sf border border-ln rounded-lg shadow-pop p-6 flex flex-col gap-4">
        <div className="flex items-center gap-2.5">
          <div className="min-w-0 grow"><PlandilloLogo /></div>
          <LangSwitch />
        </div>
        <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); if (!send.isPending) send.mutate() }}>
          <div className="flex flex-col gap-1">
            <h1 className="text-[20px] font-semibold tracking-[-.01em]">{tr(title)}</h1>
            <p className="text-sm text-t2">{tr(text)}</p>
          </div>
          {fields.map((f, i) => (
            <label key={f.key} className="flex flex-col gap-1.5">
              <span className={cap}>{tr(f.label)}</span>
              <input
                className={inputCls} required autoFocus={i === 0} autoComplete={f.autoComplete}
                type={f.password ? 'password' : 'text'} minLength={f.password && f.autoComplete === 'new-password' ? 10 : undefined}
                maxLength={f.password ? 128 : undefined} value={values[f.key] ?? ''}
                onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
              />
              {f.hint && <span className="text-xs text-t3">{tr(f.hint)}</span>}
            </label>
          ))}
          {message && <div className="text-rd text-[13px]" role="alert">{message}</div>}
          <div className="flex justify-end">
            <button type="submit" className={btnPri} disabled={send.isPending}>{tr(submit)}<Icon name="arrowRight" size={15} sw={2} /></button>
          </div>
        </form>
        {children}
      </div>
    </main>
  )
}

const username: FieldDef = { key: 'username', label: 'Имя пользователя', autoComplete: 'username' }
const newPassword: FieldDef = { key: 'password', label: 'Пароль', password: true, autoComplete: 'new-password', hint: 'Не короче 10 символов.' }

function Login({ open, passkeys }: { open: boolean; passkeys: boolean }) {
  const [challenge, setChallenge] = useState('')
  if (challenge) return <AuthForm key="totp" title="Код аутентификатора" text="Введите шестизначный код из приложения." submit="Войти" fields={[{key:'code',label:'Код',autoComplete:'one-time-code'}]} onSubmit={v=>api.finishTOTP(challenge,v.code)}>
    <button type="button" onClick={()=>setChallenge('')}>{tr('Назад')}</button>
  </AuthForm>
  return (
    <AuthForm
      title="Вход" text="Войдите, чтобы открыть свои проекты." submit="Войти"
      fields={[username, { key: 'password', label: 'Пароль', password: true, autoComplete: 'current-password' }]}
      onSubmit={async v => {const result=await api.login(v.username,v.password);if ("challenge" in result) setChallenge(result.challenge)}}
    >{passkeys && <PasskeyLogin />}<nav className="flex justify-between gap-3 text-sm text-act"><Link to="/recover">{tr('Забыли пароль?')}</Link>{open && <Link to="/register">{tr('Создать аккаунт')}</Link>}</nav></AuthForm>
  )
}

type IssuedCode = { id?: number; recovery_code: string }
type Saved = (response: IssuedCode) => void

function Setup({ saved, fromEnvironment }: { saved: Saved; fromEnvironment: boolean }) {
  return (
    <AuthForm
      title="Первый запуск" text="Создайте администратора. Он сможет приглашать других пользователей." submit="Создать"
      fields={[
        { key: 'code', label: 'Код настройки', autoComplete: 'off', hint: fromEnvironment ? 'Введите код из переменной PLANDILLO_SETUP_CODE в настройках проекта Vercel.' : 'Получите код командой plandillo setup-code на сервере. В Docker выполните docker compose -f compose.selfhost.yml exec docket /app/plandillo setup-code.' },
        username, newPassword,
      ]}
      onSubmit={(v) => api.setup(v.code, v.username, v.password).then(saved)}
    />
  )
}

function Guest({ path, saved }: { path: string; saved: Saved }) {
  const policy = useQuery({queryKey: ['signup-mode'], queryFn: api.registration, retry: 1})
  const back = <Link className="text-act text-sm" to="/">{tr('Войти')}</Link>
  if (path === '/recover') return <AuthForm title="Восстановление доступа" text="Введите резервный код и новый пароль. После восстановления нужно будет заново добавить ключи доступа." submit="Восстановить доступ"
    fields={[username,{ key: 'code', label: 'Резервный код', autoComplete: 'off' },newPassword]}
    onSubmit={v => api.recover(v.username,v.code,v.password).then(saved)}>{back}</AuthForm>
  const invite = path.startsWith('/invite/')
  if (invite || path === '/register') {
    if (policy.isPending) return null
    if (policy.data?.mode === 'open' || (invite && policy.data?.mode === 'invite')) {
      return <AuthForm title={invite ? 'Приглашение' : 'Создать аккаунт'} text="После регистрации откроется ваше личное рабочее пространство." submit="Создать аккаунт"
        fields={[username,newPassword]} onSubmit={v => (invite ? api.acceptInvite(path.slice(8),v.username,v.password) : api.register(v.username,v.password)).then(saved)}>{back}</AuthForm>
    }
    return <main className="min-h-screen grid place-content-center gap-4"><p>{tr('Регистрация сейчас недоступна')}</p>{back}</main>
  }
  return <Login open={policy.data?.mode === 'open'} passkeys={policy.data?.passkeys === true} />
}

export function Gate({ children }: { children: ReactNode }) {
  const me = useMe()
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
  const loggedOut = me.isSuccess && me.data.mode === 'selfhost' && !me.data.user && !me.data.setup

  // App sets page-specific titles that would go stale behind the login screen.
  const inApp = me.isSuccess && (me.data?.mode === 'local' || !!me.data?.user)
  useEffect(() => {
    if (!inApp) document.title = 'Plandillo'
  }, [inApp])

  // Signed in already: drop /invite/<token> from the URL and history instead of letting the app print it as "not found".
  const leaving = !loggedOut && !!me.data?.user && (path.startsWith('/invite/') || path === '/register' || path === '/recover')
  useEffect(() => {
    if (leaving) navigate('/', true)
  }, [leaving])

  const wrap = (el: ReactNode) => <div className="min-h-screen bg-bg text-tx">{el}</div>
  const saved: Saved = ({ recovery_code, id }) => { setIssued({ recovery_code, id }); navigate('/', true) }
  // Registration signs in the new owner, while recovery issues a code on the guest screen.
  if (issued && (issued.id === undefined ? loggedOut : me.isSuccess && me.data.user?.id === issued.id)) return wrap(<main className="min-h-screen grid place-items-center p-4"><section className="w-full max-w-md bg-sf border border-ln rounded-lg p-6 grid gap-4">
    <h1 className="font-semibold">{tr('Сохраните резервный код')}</h1><p>{tr('Этот одноразовый код позволит задать новый пароль. Сохраните его в менеджере паролей.')}</p>
    <code className="select-all break-all">{issued.recovery_code}</code><button className={btnPri} onClick={() => setIssued(null)}>{tr('Код сохранён')}</button>
  </section></main>)
  if (me.isPending) return <div className="min-h-screen bg-bg" /> // no flash of the app
  if (loggedOut) {
    return wrap(<Guest key={path} path={path} saved={saved} />)
  }
  if (leaving) return null
  if (inApp) return <>{children}</>
  if (me.data?.setup) return wrap(<Setup fromEnvironment={me.data?.setup_source === 'environment'} saved={saved} />)
  return wrap(
    <main className="min-h-screen grid place-items-center px-4 text-center">
      <div className="flex flex-col items-center gap-3">
        <p className="text-sm text-t2" role="alert">{tr('Не удалось связаться с сервером')}{me.error ? `: ${errText(me.error)}` : ''}</p>
        <button type="button" className={btnPri} onClick={() => me.refetch()}>{tr('Повторить')}</button>
      </div>
    </main>,
  )
}
