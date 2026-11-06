// Self-host gate: asks the server who we are before <App /> mounts (so no app hook or SSE runs before login) and
// shows setup, login or invite screens. In local mode, or with a session, it renders the app untouched.
import { type ReactNode, useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ApiError, type Me, api, useMe } from './api'
import { Icon } from './icons'
import { navigate, useRoute } from './router'
import { useTheme } from './theme'
import { btnPri, cap } from './ui'

const inputCls = 'h-[38px] border border-ln rounded-md bg-sf px-3 text-[14px] text-tx focus:border-ring focus:ring-[3px] focus:ring-ring/50 focus:outline-none'

type FieldDef = { key: string; label: string; password?: boolean; autoComplete: string; hint?: string }

// One card for all three screens: the fields, a submit button and the server's error.
function AuthForm({ title, text, fields, submit, onSubmit }: {
  title: string; text: string; fields: FieldDef[]; submit: string; onSubmit: (v: Record<string, string>) => Promise<unknown>
}) {
  const qc = useQueryClient()
  const [values, setValues] = useState<Record<string, string>>({})
  const send = useMutation({
    mutationFn: () => onSubmit(values),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['me'] }),
  })
  const err = send.error
  const message = err instanceof ApiError && err.code === 'rate_limited' ? 'Слишком много попыток, подождите несколько минут.' : err?.message
  return (
    <main className="min-h-screen grid place-items-center px-4 py-8">
      <div className="w-full max-w-[460px] bg-sf border border-ln rounded-lg shadow-pop p-6 flex flex-col gap-4">
        <div className="flex items-center gap-2.5">
          <span className="size-8 rounded-md bg-ink text-inkt grid place-items-center"><Icon name="logo" size={19} sw={2} /></span>
          <b className="text-[17px] font-bold tracking-[-.01em] grow">Docket</b>
        </div>
        <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); if (!send.isPending) send.mutate() }}>
          <div className="flex flex-col gap-1">
            <h1 className="text-[20px] font-semibold tracking-[-.01em]">{title}</h1>
            <p className="text-sm text-t2">{text}</p>
          </div>
          {fields.map((f, i) => (
            <label key={f.key} className="flex flex-col gap-1.5">
              <span className={cap}>{f.label}</span>
              <input
                className={inputCls} required autoFocus={i === 0} autoComplete={f.autoComplete}
                type={f.password ? 'password' : 'text'} minLength={f.password && f.autoComplete === 'new-password' ? 10 : undefined}
                maxLength={f.password ? 128 : undefined} value={values[f.key] ?? ''}
                onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
              />
              {f.hint && <span className="text-xs text-t3">{f.hint}</span>}
            </label>
          ))}
          {message && <div className="text-rd text-[13px]" role="alert">{message}</div>}
          <div className="flex justify-end">
            <button type="submit" className={btnPri} disabled={send.isPending}>{submit}<Icon name="arrowRight" size={15} sw={2} /></button>
          </div>
        </form>
      </div>
    </main>
  )
}

const username: FieldDef = { key: 'username', label: 'Имя пользователя', autoComplete: 'username' }
const newPassword: FieldDef = { key: 'password', label: 'Пароль', password: true, autoComplete: 'new-password', hint: 'Не короче 10 символов.' }

function Login() {
  return (
    <AuthForm
      title="Вход" text="Войдите, чтобы открыть свои проекты." submit="Войти"
      fields={[username, { key: 'password', label: 'Пароль', password: true, autoComplete: 'current-password' }]}
      onSubmit={(v) => api.login(v.username, v.password)}
    />
  )
}

function Setup() {
  return (
    <AuthForm
      title="Первый запуск" text="Создайте администратора. Он сможет приглашать других пользователей." submit="Создать"
      fields={[
        { key: 'code', label: 'Код настройки', autoComplete: 'off', hint: 'Код напечатан в логе сервера при первом запуске.' },
        username, newPassword,
      ]}
      onSubmit={(v) => api.setup(v.code, v.username, v.password)}
    />
  )
}

function AcceptInvite({ token }: { token: string }) {
  const qc = useQueryClient()
  return (
    <AuthForm
      title="Приглашение" text="Вас пригласили в Docket. Придумайте имя пользователя и пароль." submit="Создать аккаунт"
      fields={[username, newPassword]}
      // Seed `me` before leaving /invite/ so Login does not flash while it refetches.
      onSubmit={async (v) => {
        const user = await api.acceptInvite(token, v.username, v.password)
        qc.setQueryData<Me>(['me'], { mode: 'selfhost', user })
        navigate('/', true)
      }}
    />
  )
}

export function Gate({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const theme = useTheme()
  const me = useMe()
  const { path } = useRoute()

  // Session ended (expired, or logout): forget everything cached for the previous profile and put `me` back to
  // pending, which unmounts <App /> (closing its observers and the SSE stream) while `me` is asked again.
  // invalidate would keep the old user in `data` (a failed v5 refetch keeps it) and the app would stay mounted.
  useEffect(() => {
    const reset = () => {
      if (qc.getQueryState(['me'])?.data === undefined) return // already pending or on the login screen: nothing to drop
      qc.removeQueries({ predicate: (q) => q.queryKey[0] !== 'me' })
      qc.resetQueries({ queryKey: ['me'] }) // the /api/auth/me request never dispatches the event, so this cannot loop
    }
    window.addEventListener('docket:unauthorized', reset)
    return () => window.removeEventListener('docket:unauthorized', reset)
  }, [qc])

  const wrap = (el: ReactNode) => <div className="min-h-screen bg-bg text-tx" data-theme={theme.resolved}>{el}</div>
  if (me.isPending) return <div className="min-h-screen bg-bg" data-theme={theme.resolved} /> // no flash of the app
  if (me.error instanceof ApiError && me.error.code === 'unauthorized') { // before the user check: v5 keeps stale data on error
    return wrap(path.startsWith('/invite/') ? <AcceptInvite token={path.slice('/invite/'.length)} /> : <Login />)
  }
  if (me.data?.mode === 'local' || me.data?.user) return <>{children}</>
  if (me.data?.setup) return wrap(<Setup />)
  return wrap(
    <main className="min-h-screen grid place-items-center px-4 text-center">
      <div className="flex flex-col items-center gap-3">
        <p className="text-sm text-t2" role="alert">Не удалось связаться с сервером{me.error ? `: ${me.error.message}` : ''}</p>
        <button type="button" className={btnPri} onClick={() => me.refetch()}>Повторить</button>
      </div>
    </main>,
  )
}
