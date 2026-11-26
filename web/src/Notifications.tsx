// Settings → This device → App: Web Push for this device (server: PLANDILLO_PUSH=on, internal/push).
import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errText } from './api'
import { getLang, tr } from './i18n'
import { SettingsRow, SettingsSection, Toggle, textInput } from './ui'

const supported = 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window

// The VAPID key as the bytes pushManager.subscribe takes (base64url → Uint8Array).
function keyBytes(b64: string) {
  const s = atob(b64.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (b64.length % 4)) % 4))
  return Uint8Array.from(s, (c) => c.charCodeAt(0))
}

export function Notifications() {
  const qc = useQueryClient()
  const [sub, setSub] = useState<PushSubscription | null>() // undefined while the browser answers
  useEffect(() => {
    if (!supported) return
    void navigator.serviceWorker.ready.then((r) => r.pushManager.getSubscription()).then(setSub, () => setSub(null))
  }, [])
  const state = useQuery({ queryKey: ['push', sub?.endpoint ?? ''], queryFn: () => api.pushState(sub?.endpoint ?? ''), enabled: supported && sub !== undefined })
  const saved = state.data?.subscription
  const [time, setTime] = useState('')
  const morning = saved?.morning ?? ''

  const apply = useMutation({
    mutationFn: async (next: { agent_done: boolean; morning: string }) => {
      if (!next.agent_done && !next.morning) { // nothing left to send: unsubscribe this device
        if (sub) { await api.removePush(sub.endpoint); await sub.unsubscribe(); setSub(null) }
        return
      }
      let s = sub
      if (!s) {
        if ((await Notification.requestPermission()) !== 'granted') throw new Error(tr('Уведомления запрещены в настройках браузера для этого сайта.'))
        const reg = await navigator.serviceWorker.ready
        s = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(state.data!.public_key!) })
        setSub(s)
      }
      const keys = s.toJSON().keys ?? {}
      await api.savePush({ endpoint: s.endpoint, p256dh: keys.p256dh ?? '', auth: keys.auth ?? '', ...next, tz: Intl.DateTimeFormat().resolvedOptions().timeZone, lang: getLang() })
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['push'] }),
  })
  const set = (next: Partial<{ agent_done: boolean; morning: string }>) => apply.mutate({ agent_done: saved?.agent_done ?? false, morning, ...next })

  let body
  if (!supported) body = <p className="text-sm text-t2">{tr('Этот браузер не поддерживает push-уведомления. На iPhone и iPad они работают в Plandillo, добавленном на экран «Домой».')}</p>
  else if (state.data && !state.data.enabled) body = <p className="text-sm text-t2">{tr('На этом сервере push-уведомления выключены. Администратор включает их параметром PLANDILLO_PUSH=on.')}</p>
  if (body) return <SettingsSection title={tr('Уведомления')}><SettingsRow label={tr('Push-уведомления')}>{body}</SettingsRow></SettingsSection>

  const busy = !state.data || apply.isPending
  return (
    <SettingsSection title={tr('Уведомления')} hint={tr('Для этого устройства. Приходят, даже когда Plandillo закрыт.')}>
      <SettingsRow label={tr('Агент завершил задачу')} hint={tr('Когда агент переводит задачу в выполненные.')}>
        <div><Toggle label={tr('Агент завершил задачу')} on={!!saved?.agent_done} disabled={busy} onChange={(v) => set({ agent_done: v })} /></div>
      </SettingsRow>
      <SettingsRow label={tr('Утреннее напоминание')} hint={tr('План на сегодня в выбранное время, по часам этого устройства.')}>
        <div className="flex items-center gap-3">
          <Toggle label={tr('Утреннее напоминание')} on={!!morning} disabled={busy} onChange={(v) => set({ morning: v ? time || '09:00' : '' })} />
          <input type="time" className={textInput + ' w-32'} aria-label={tr('Время напоминания')} disabled={busy} value={time || morning || '09:00'}
            onChange={(e) => setTime(e.target.value)} onBlur={() => { if (morning && time && time !== morning) set({ morning: time }) }} />
        </div>
      </SettingsRow>
      {(apply.isError || state.isError) && <div className="border-t border-ln px-5 py-3 text-[13px] text-rd" role="alert">{errText(apply.error ?? state.error)}</div>}
    </SettingsSection>
  )
}
