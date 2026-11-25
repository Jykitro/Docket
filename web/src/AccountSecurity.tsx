import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errText, useMe } from './api'
import { thumbnail } from './ProjectIconPicker'
import { tr, locale } from './i18n'
import { btn, btnPri, cap } from './ui'

const field = 'rounded-md border border-ln bg-sf px-2 py-1.5'
export function ProfilePhoto() {
  const me = useMe().data?.user
  const qc = useQueryClient()
  const update = useMutation({
    mutationFn: async (file: File | null) => api.setPhoto(file ? await thumbnail(file) : null),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['me'] }),
  })
  return (
    <div className="flex flex-wrap items-center gap-2">
      {me?.photo && <img src={me.photo + '?v=' + me.updated_at} className="size-9 rounded-md" alt="" />}
      <label className={btn}>
        {tr('Фото профиля')}
        <input
          className="sr-only"
          type="file"
          accept="image/png,image/jpeg,image/webp,image/svg+xml"
          disabled={update.isPending}
          onChange={(e) => {
            const file = e.target.files?.[0]
            if (file) update.mutate(file)
            e.target.value = ''
          }}
        />
      </label>
      {me?.photo && (
        <button className={btn} disabled={update.isPending} onClick={() => update.mutate(null)}>
          {tr('Удалить фото')}
        </button>
      )}
      {update.isError && <p role="alert">{errText(update.error)}</p>}
    </div>
  )
}
export function AccountSecurity() {
  const qc = useQueryClient()
  const sessions = useQuery({ queryKey: ['devices'], queryFn: api.sessions })
  const status = useQuery({ queryKey: ['totp'], queryFn: api.totp })
  const [password, setPassword] = useState(''),
    [code, setCode] = useState('')
  const [open, setOpen] = useState(false)
  const [setup, setSetup] = useState<Awaited<ReturnType<typeof api.beginTOTP>>>()
  const factor = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      if (!setup && !status.data?.enabled) {
        setSetup(await api.beginTOTP(password))
        return
      }
      await api.setTOTP(!status.data?.enabled, password, code)
      setSetup(undefined)
      setPassword('')
      setCode('')
      setOpen(false)
      await qc.invalidateQueries({ queryKey: ['totp'] })
    },
  })
  const revoke = useMutation({
    mutationFn: api.revokeSession,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['devices'] }),
  })
  return (
    <section className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-2 border-t border-ln pt-3">
      <h3 className={cap}>{tr('Двухфакторный вход')}</h3>
      {status.data?.available && !open && (
        <button className={btn} onClick={() => setOpen(true)}>
          {tr(status.data.enabled ? 'Отключить аутентификатор' : 'Подключить аутентификатор')}
        </button>
      )}
      {status.data && !status.data.available && (
        <p className="text-sm text-t2">{tr('Администратору нужно настроить ключ шифрования TOTP.')}</p>
      )}
      {open && (
        <form
          className="grid min-w-0 gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            factor.mutate()
          }}
        >
          <label>
            {tr('Пароль')}
            <input
              className={field + ' w-full'}
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </label>
          {setup && (
            <>
              <img
                className="size-40"
                src={'data:image/png;base64,' + setup.qr_png}
                alt={tr('QR-код аутентификатора')}
              />
              <code className="break-all select-all">{setup.secret}</code>
            </>
          )}
          {(setup || status.data?.enabled) && (
            <label>
              {tr('Код аутентификатора')}
              <input
                className={field + ' w-full'}
                autoComplete="one-time-code"
                inputMode="numeric"
                required
                pattern="[0-9]{6}"
                value={code}
                onChange={(e) => setCode(e.target.value)}
              />
            </label>
          )}
          <div className="flex gap-2">
            <button className={btnPri} disabled={factor.isPending}>
              {tr('Продолжить')}
            </button>
            <button
              className={btn}
              type="button"
              onClick={() => {
                setOpen(false)
                setSetup(undefined)
                setPassword('')
                setCode('')
              }}
            >
              {tr('Отмена')}
            </button>
          </div>
        </form>
      )}
      <h3 className={cap}>{tr('Активные сессии')}</h3>
      {sessions.data?.map((s) => (
        <div key={s.id} className="flex min-w-0 items-center gap-2 border-b border-ln py-2 text-sm">
          <div className="min-w-0 flex-1">
            <strong>{tr(s.current ? 'Это устройство' : 'Устройство')}</strong>
            <p className="truncate text-xs text-t2" title={s.user_agent}>
              {s.user_agent}
            </p>
            <time className="text-xs text-t2">{new Date(s.last_seen_at).toLocaleString(locale())}</time>
          </div>
          <button
            className={btn}
            disabled={revoke.isPending}
            onClick={() =>
              revoke.mutate(s.id, {
                onSuccess: () => {
                  if (s.current) location.replace('/')
                },
              })
            }
          >
            {tr('Завершить')}
          </button>
        </div>
      ))}
      {[sessions.error, status.error, factor.error, revoke.error].filter(Boolean).map((e, i) => (
        <p role="alert" key={i}>
          {errText(e)}
        </p>
      ))}
    </section>
  )
}
