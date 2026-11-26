import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errText } from './api'
import { Icon } from './icons'
import { tr } from './i18n'
import { btn, btnPri, cap } from './ui'

function supported() {
  return window.isSecureContext && typeof PublicKeyCredential !== 'undefined'
    && typeof PublicKeyCredential.parseCreationOptionsFromJSON === 'function'
    && typeof PublicKeyCredential.parseRequestOptionsFromJSON === 'function'
    && typeof PublicKeyCredential.prototype.toJSON === 'function'
}

// Closing settings or leaving the login screen dismisses the outstanding device prompt.
function usePasskeyPrompt() {
  const active = useRef<AbortController | null>(null)
  useEffect(() => () => active.current?.abort(), [])
  return () => {
    active.current?.abort()
    active.current = new AbortController()
    return active.current.signal
  }
}

function passkeyError(error: unknown) {
  if (error instanceof DOMException) {
    if (error.name === 'NotAllowedError' || error.name === 'AbortError') return tr('Подтверждение отменено или время ожидания истекло.')
    if (error.name === 'InvalidStateError') return tr('Этот ключ уже добавлен.')
    return tr('Не удалось использовать ключ доступа на этом устройстве.')
  }
  return errText(error)
}

export function PasskeyLogin() {
  const qc = useQueryClient()
  const startPrompt = usePasskeyPrompt()
  const login = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      const signal = startPrompt()
      const { publicKey } = await api.beginPasskeyLogin()
      const key = await navigator.credentials.get({ signal, publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(publicKey) })
      if (!(key instanceof PublicKeyCredential)) throw new DOMException('', 'NotAllowedError')
      return api.finishPasskeyLogin(key.toJSON())
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['me'] }),
  })
  if (!supported()) return null
  return <div className="flex flex-col gap-5">
    <div className="flex flex-col gap-2">
      <button type="button" className="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md border border-ln2 bg-sf text-sm font-medium text-tx shadow-xs hover:bg-sk disabled:opacity-50" disabled={login.isPending} onClick={() => login.mutate()}>
        <span className="text-t2"><Icon name="passkey" size={18} sw={1.7} /></span>{tr('Войти с ключом доступа')}
      </button>
      {login.isError && <p className="text-rd text-sm" role="alert">{passkeyError(login.error)}</p>}
    </div>
    <div className="flex items-center gap-3 text-[12.5px] text-t3"><span aria-hidden="true" className="h-px flex-1 bg-ln" />{tr('или паролем')}<span aria-hidden="true" className="h-px flex-1 bg-ln" /></div>
  </div>
}

export function PasskeySettings() {
  const qc = useQueryClient()
  const startPrompt = usePasskeyPrompt()
  const list = useQuery({ queryKey: ['passkeys'], queryFn: api.passkeys })
  const [action, setAction] = useState<{ kind: 'add' } | { kind: 'remove'; id: string; name: string } | null>(null)
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const change = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      if (!action) return
      if (action.kind === 'remove') return api.revokePasskey(action.id, password)
      const signal = startPrompt()
      const { publicKey } = await api.beginPasskeyRegistration(name.trim(), password)
      const key = await navigator.credentials.create({ signal, publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(publicKey) })
      if (!(key instanceof PublicKeyCredential)) throw new DOMException('', 'NotAllowedError')
      return api.finishPasskeyRegistration(key.toJSON())
    },
    onSuccess: async () => {
      setAction(null)
      setPassword('')
      setName('')
      await qc.invalidateQueries({ queryKey: ['passkeys'] })
    },
  })
  function select(next: typeof action) {
    change.reset()
    setPassword('')
    setName('')
    setAction(next)
  }
  const input = 'h-9 w-full rounded-md border border-ln bg-sf px-2.5 text-sm'
  return <section className="grid gap-2" aria-label={tr('Ключи доступа')}>
    <h3 className={cap}>{tr('Ключи доступа')}</h3>
    {list.isError && <p role="alert" className="text-sm text-rd">{errText(list.error)}</p>}
    {list.data?.keys.map(key => <div key={key.id} className="flex items-center gap-2 text-sm">
      <span className="min-w-0 grow break-words">{key.name}</span>
      <button type="button" className={btn} disabled={change.isPending} aria-label={tr('Отозвать ключ {name}', { name: key.name })} onClick={() => select({ kind: 'remove', ...key })}>{tr('Отозвать')}</button>
    </div>)}
    {list.data && (list.data.available && supported()
      ? !action && <button type="button" className={btn + ' justify-self-start'} onClick={() => select({ kind: 'add' })}>{tr('Добавить ключ доступа')}</button>
      : <p className="text-sm text-t2">{tr('Для добавления ключа откройте настроенный адрес Plandillo в браузере с поддержкой passkey.')}</p>)}
    {action && <form className="grid gap-2" onSubmit={e => { e.preventDefault(); if (!change.isPending) change.mutate() }}>
      {action.kind === 'add'
        ? <label className="grid gap-1 text-sm">{tr('Название ключа')}<input className={input} maxLength={64} required value={name} onChange={e => setName(e.target.value)} /></label>
        : <p className="text-sm">{tr('Отозвать ключ {name}', { name: action.name })}</p>}
      <label className="grid gap-1 text-sm">{tr('Текущий пароль')}<input className={input} type="password" autoComplete="current-password" required maxLength={128} value={password} onChange={e => setPassword(e.target.value)} /></label>
      <div className="flex gap-2"><button className={btnPri} disabled={change.isPending || (action.kind === 'add' && !name.trim())}>{tr('Подтвердить')}</button>
        <button type="button" className={btn} disabled={change.isPending} onClick={() => select(null)}>{tr('Отмена')}</button></div>
      {change.isError && <p role="alert" className="text-rd text-sm">{passkeyError(change.error)}</p>}
    </form>}
  </section>
}
