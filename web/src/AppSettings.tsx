import { WorkspaceSettings } from './Workspaces'
import { AccountSecurity, ProfilePhoto } from './AccountSecurity'
import { workspaceApi } from './workspacePath'
// App-level settings dialog (the gear next to the profile): profile name, theme, and platform links.
// Per-project settings live in Settings.tsx.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, useMe, useProfile } from './api'
import { Icon } from './icons'
import { locale, tr } from './i18n'
import { NAME_MAX } from './Onboarding'
import { Link } from './router'
import { type ThemePref } from './theme'
import { Dialog, LangSwitch, btn, btnPri, cap } from './ui'
import { InstallPlandillo } from './Pwa'
import { AccountSection } from './Users'

export const THEMES: { id: ThemePref; icon: string; label: string }[] = [
  { id: 'light', icon: 'sun', label: 'Светлая' },
  { id: 'dark', icon: 'moon', label: 'Тёмная' },
  { id: 'system', icon: 'monitor', label: 'Как в системе' },
]
const choice = 'inline-flex items-center gap-1.5 h-8 px-3 rounded-md border text-[13px] '
const row = 'flex items-center gap-3 min-h-10 px-3 rounded-md border border-ln bg-sf2 text-sm no-underline text-tx hover:bg-sk'

export function AppSettings({ initial, theme, onTheme, onClose }: { initial:'Профиль'|'Пространство'; theme: ThemePref; onTheme: (t: ThemePref) => void; onClose: () => void }) {
  type Section = 'Профиль' | 'Пространство' | 'Безопасность' | 'Приложение'
  const [section,setSection]=useState<Section>(initial)
  const qc = useQueryClient()
  const profile = useProfile()
  const me = useMe().data
  const [name, setName] = useState(profile.data?.name ?? '')
  const save = useMutation({
    mutationFn: () => api.setProfile(name.trim()),
    onSuccess: () => Promise.all([qc.invalidateQueries({ queryKey: ['profile'] }),qc.invalidateQueries({queryKey:['me']})]),
  })
  const dirty = name.trim() !== '' && name.trim() !== profile.data?.name

  return (
    <Dialog title={tr('Настройки')} onClose={onClose}>
      <nav className="flex flex-wrap gap-1" aria-label={tr('Разделы настроек')}>{(['Профиль','Пространство','Безопасность','Приложение'] as const).filter(s=>s!=='Безопасность'||me?.mode==='selfhost').map(s=><button type="button" className={choice+(s===section?'bg-acs text-act border-transparent':'border-ln text-t2')} key={s} aria-pressed={s===section} onClick={()=>setSection(s)}>{tr(s)}</button>)}</nav>
      {section==='Пространство' && profile.data?.storage && <p className="text-sm text-t2">{tr('Хранилище демо: {used} из {limit} МБ', {
        used: (profile.data.storage.used / 1_000_000).toLocaleString(locale(), { maximumFractionDigits: 2 }),
        limit: profile.data.storage.limit / 1_000_000,
      })}</p>}
      {section==='Профиль' && <><form className="flex flex-col gap-1.5" onSubmit={(e) => { e.preventDefault(); dirty && !save.isPending && save.mutate() }}>
        <span className={cap}>{tr('Профиль')}</span>
        <div className="flex gap-2">
          <input className="h-9 grow min-w-0 border border-ln rounded-md bg-sf px-2.5 text-sm text-tx focus:border-ring focus:ring-[3px] focus:ring-ring focus:outline-none" maxLength={NAME_MAX} value={name} onChange={(e) => setName(e.target.value)} aria-label={tr('Имя')} />
          <button type="submit" className={btnPri} disabled={!dirty || save.isPending}>{tr('Сохранить')}</button>
        </div>
        {save.isError && <div className="text-rd text-[13px]" role="alert">{save.error.message}</div>}
      </form>
      <ProfilePhoto /></>}
      {section==='Пространство' && <WorkspaceSettings />}
      {section==='Безопасность' && <AccountSecurity />}
      {section==='Безопасность' && me?.mode === 'selfhost' && me.user && <AccountSection user={me.user} />}
      {section==='Приложение' && <><div className="flex flex-col gap-1.5">
        <span className={cap}>{tr('Язык')}</span>
        <div><LangSwitch /></div>
      </div>
      <div className="flex flex-col gap-1.5">
        <span className={cap}>{tr('Тема')}</span>
        <div className="flex flex-wrap gap-1.5" role="group" aria-label={tr('Тема')}>
          {THEMES.map((t) => (
            <button key={t.id} type="button" className={choice + (theme === t.id ? 'bg-acs border-transparent text-act font-medium' : 'bg-sf border-ln text-t2 hover:bg-sk')} aria-pressed={theme === t.id} onClick={() => onTheme(t.id)}>
              <Icon name={t.icon} size={14} sw={2} />{tr(t.label)}
            </button>
          ))}
        </div>
      </div>
      <div className="flex flex-col gap-1.5">
        <span className={cap}>{tr('Платформа')}</span>
        <InstallPlandillo />
        <Link to="/integrations" className={row} onClick={onClose}><Icon name="plug" /><span className="grow">{tr('Интеграции: MCP, Code Mode, токен и доступ')}</span><Icon name="chevR" size={14} /></Link>
        <Link to="/trash" className={row} onClick={onClose}><Icon name="trash" /><span className="grow">{tr('Корзина')}</span><Icon name="chevR" size={14} /></Link>
        <a className={row} href={workspaceApi("/api/export")} download><Icon name="stack" /><span className="grow">{tr('Скачать все данные (JSON)')}</span><Icon name="arrowRight" size={14} /></a>
        <div className="flex items-center gap-3 px-3 text-[12.5px] text-t3"><span className="grow">{tr('Адрес')}</span><span className="font-mono">{location.host}</span></div>
      </div>
      </>}
      <div className="flex justify-end">
        <button type="button" className={btn} onClick={onClose}>{tr('Закрыть')}</button>
      </div>
    </Dialog>
  )
}
