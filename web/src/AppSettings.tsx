// App-level settings dialog (the gear next to the profile): profile name, theme, and platform links.
// Per-project settings live in Settings.tsx.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, useProfile } from './api'
import { Icon } from './icons'
import { NAME_MAX } from './Onboarding'
import { Link } from './router'
import { type ThemePref } from './theme'
import { Dialog, btn, btnPri, cap } from './ui'

export const THEMES: { id: ThemePref; icon: string; label: string }[] = [
  { id: 'light', icon: 'sun', label: 'Светлая' },
  { id: 'dark', icon: 'moon', label: 'Тёмная' },
  { id: 'system', icon: 'monitor', label: 'Как в системе' },
]
const choice = 'inline-flex items-center gap-1.5 h-8 px-3 rounded-md border text-[13px] '
const row = 'flex items-center gap-3 min-h-10 px-3 rounded-md border border-ln bg-sf2 text-sm no-underline text-tx hover:bg-sk'

export function AppSettings({ theme, onTheme, onClose }: { theme: ThemePref; onTheme: (t: ThemePref) => void; onClose: () => void }) {
  const qc = useQueryClient()
  const profile = useProfile()
  const [name, setName] = useState(profile.data?.name ?? '')
  const save = useMutation({
    mutationFn: () => api.setProfile(name.trim()),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['profile'] }),
  })
  const dirty = name.trim() !== '' && name.trim() !== profile.data?.name

  return (
    <Dialog title="Настройки" onClose={onClose}>
      <form className="flex flex-col gap-1.5" onSubmit={(e) => { e.preventDefault(); dirty && !save.isPending && save.mutate() }}>
        <span className={cap}>Профиль</span>
        <div className="flex gap-2">
          <input className="h-9 grow min-w-0 border border-ln rounded-md bg-sf px-2.5 text-sm text-tx focus:border-ring focus:ring-[3px] focus:ring-ring/50 focus:outline-none" maxLength={NAME_MAX} value={name} onChange={(e) => setName(e.target.value)} aria-label="Имя" />
          <button type="submit" className={btnPri} disabled={!dirty || save.isPending}>Сохранить</button>
        </div>
        {save.isError && <div className="text-rd text-[13px]" role="alert">{save.error.message}</div>}
      </form>
      <div className="flex flex-col gap-1.5">
        <span className={cap}>Тема</span>
        <div className="flex flex-wrap gap-1.5" role="group" aria-label="Тема">
          {THEMES.map((t) => (
            <button key={t.id} type="button" className={choice + (theme === t.id ? 'bg-acs border-transparent text-act font-medium' : 'bg-sf border-ln text-t2 hover:bg-sk')} aria-pressed={theme === t.id} onClick={() => onTheme(t.id)}>
              <Icon name={t.icon} size={14} sw={2} />{t.label}
            </button>
          ))}
        </div>
      </div>
      <div className="flex flex-col gap-1.5">
        <span className={cap}>Платформа</span>
        <Link to="/mcp" className={row} onClick={onClose}><Icon name="plug" /><span className="grow">Подключение Claude (MCP), токен и доступ</span><Icon name="chevR" size={14} /></Link>
        <Link to="/trash" className={row} onClick={onClose}><Icon name="trash" /><span className="grow">Корзина</span><Icon name="chevR" size={14} /></Link>
        <a className={row} href="/api/export" download><Icon name="stack" /><span className="grow">Скачать все данные (JSON)</span><Icon name="arrowRight" size={14} /></a>
        <div className="flex items-center gap-3 px-3 text-[12.5px] text-t3"><span className="grow">Адрес</span><span className="font-mono">{location.host}</span></div>
      </div>
      <div className="flex justify-end">
        <button type="button" className={btn} onClick={onClose}>Закрыть</button>
      </div>
    </Dialog>
  )
}
