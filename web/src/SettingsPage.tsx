// Settings as pages (/settings/<section>), grouped the way people think about them: their account, the current
// workspace, and the app on this device. The sidebar swaps to SettingsNav while a settings page is open.
// Per-project settings stay in Settings.tsx (/p/<slug>/settings).
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type Me, api, errText, useMe, useProfile } from './api'
import { AccountSecurity, ProfilePhoto } from './AccountSecurity'
import { Icon } from './icons'
import { LANGS, locale, setLang, tr, useLang } from './i18n'
import { NAME_MAX } from './Onboarding'
import { InstallPlandillo } from './Pwa'
import { Link } from './router'
import { type ThemePref } from './theme'
import { AccountSection, Users } from './Users'
import { WorkspaceGeneral, WorkspaceList } from './Workspaces'
import { Avatar, ErrorState, MENUITEM, MENUSEP, Menu, SettingsRow, SettingsSection, SkeletonRows, btn, btnPri, cap, textInput } from './ui'

export const THEMES: { id: ThemePref; icon: string; label: string }[] = [
  { id: 'light', icon: 'sun', label: 'Светлая' },
  { id: 'dark', icon: 'moon', label: 'Тёмная' },
  { id: 'system', icon: 'monitor', label: 'Как в системе' },
]

type Section = { id: string; label: string; icon: string; group: string; hint: string; when?: (me?: Me) => boolean }
const selfhost = (me?: Me) => me?.mode === 'selfhost'
const SECTIONS: Section[] = [
  { id: 'profile', group: 'Аккаунт', label: 'Профиль', icon: 'user', hint: 'Как вас видят в задачах, заметках и истории.' },
  { id: 'security', group: 'Аккаунт', label: 'Безопасность', icon: 'key', hint: 'Вход, пароль, ключи доступа и активные сессии.', when: selfhost },
  { id: 'workspace', group: 'Пространство', label: 'Общие', icon: 'sliders', hint: 'Название, иконка и данные текущего пространства.' },
  { id: 'workspaces', group: 'Пространство', label: 'Все пространства', icon: 'stack', hint: 'Создавайте, открывайте и архивируйте пространства.' },
  { id: 'appearance', group: 'Система', label: 'Оформление', icon: 'sun', hint: 'Тема и язык интерфейса на этом устройстве.' },
  { id: 'app', group: 'Система', label: 'Приложение', icon: 'monitor', hint: 'Установка, адрес сервера и ссылки на проект.' },
  { id: 'backups', group: 'Система', label: 'Бэкапы', icon: 'clock', hint: 'Копии базы данных сервера: создайте новую или скачайте готовую.', when: (me) => !!me?.user?.is_admin },
  { id: 'users', group: 'Система', label: 'Пользователи', icon: 'list', hint: 'Регистрация, приглашения и учётные записи сервера.', when: (me) => !!me?.user?.is_admin },
]
const GROUPS = ['Аккаунт', 'Пространство', 'Система']

const nv = 'flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-sm text-tx no-underline hover:bg-sk '

/** Replaces the app navigation in the sidebar while a settings page is open. */
export function SettingsNav({ section, rail }: { section: string; rail: boolean }) {
  const me = useMe().data
  const visible = SECTIONS.filter((s) => !s.when || s.when(me))
  const cls = (on: boolean) => nv + (on ? 'bg-sk font-medium' : '') + (rail ? ' justify-center px-0' : '')
  return (
    <nav className="flex flex-col gap-3" aria-label={tr('Настройки')}>
      <Link to="/" className={cls(false) + ' text-t2'} title={rail ? tr('Назад к задачам') : undefined}>
        <Icon name="chevL" />{!rail && <span>{tr('Назад к задачам')}</span>}
      </Link>
      {GROUPS.map((g) => (
        <div key={g} className="flex flex-col gap-0.5">
          {!rail && <div className={cap + ' flex h-8 items-center px-2'}>{tr(g)}</div>}
          {visible.filter((s) => s.group === g).map((s) => (
            <Link key={s.id} to={'/settings/' + s.id} className={cls(s.id === section)} aria-current={s.id === section ? 'page' : undefined} title={rail ? tr(s.label) : undefined}>
              <Icon name={s.icon} />{!rail && <span className="truncate">{tr(s.label)}</span>}
            </Link>
          ))}
        </div>
      ))}
    </nav>
  )
}

/** The account menu behind the avatar (sidebar footer and top bar). */
export function UserMenu({ variant, rail }: { variant: 'sidebar' | 'header'; rail?: boolean }) {
  const me = useMe().data
  const name = useProfile().data?.name ?? ''
  const photo = me?.user?.photo ? me.user.photo + '?v=' + me.user.updated_at : null
  const role = me?.mode === 'selfhost' ? (me.user?.is_admin ? tr('администратор') : tr('пользователь')) : tr('локально · без входа')
  const logout = useMutation({ mutationFn: api.logout }) // api.logout itself returns to the login screen
  const item = MENUITEM + ' text-tx no-underline'
  const label = variant === 'header' ? <Avatar name={name} photo={photo} /> : (
    <>
      <Avatar name={name} photo={photo} />
      {!rail && (
        <>
          <span className="flex min-w-0 grow flex-col leading-tight">
            <b className="truncate text-sm font-semibold">{name || tr('Профиль')}</b>
            <span className="truncate text-xs text-t3">{role}</span>
          </span>
          <span className="text-t3"><Icon name="chevUD" size={16} /></span>
        </>
      )}
    </>
  )
  return (
    <Menu block={variant === 'sidebar'} up={variant === 'sidebar'} right={variant === 'header'} ariaLabel={tr('Профиль и настройки')}
      className={variant === 'header' ? 'grid size-8 place-items-center rounded-full hover:ring-4 hover:ring-sk aria-expanded:ring-4 aria-expanded:ring-sk'
        : 'flex w-full items-center gap-2 rounded-md p-2 text-left hover:bg-sk aria-expanded:bg-sk ' + (rail ? 'justify-center px-0' : '')}
      label={label}>
      {(close) => (
        <div className="w-60">
          <div className="flex items-center gap-2.5 px-2 py-2">
            <Avatar name={name} photo={photo} size={36} />
            <span className="flex min-w-0 flex-col leading-tight">
              <b className="truncate text-sm font-semibold">{name || tr('Профиль')}</b>
              <span className="truncate text-xs text-t3">{me?.user?.username ?? role}</span>
            </span>
          </div>
          <div className={MENUSEP} />
          <Link to="/settings/profile" className={item} onClick={close}><Icon name="user" size={15} />{tr('Профиль')}</Link>
          <Link to="/settings/workspace" className={item} onClick={close}><Icon name="sliders" size={15} />{tr('Настройки пространства')}</Link>
          <Link to="/settings/appearance" className={item} onClick={close}><Icon name="sun" size={15} />{tr('Оформление')}</Link>
          {me?.mode === 'selfhost' && (
            <>
              <div className={MENUSEP} />
              <button type="button" className={item} disabled={logout.isPending} onClick={() => logout.mutate()}><Icon name="arrowRight" size={15} />{tr('Выйти')}</button>
            </>
          )}
        </div>
      )}
    </Menu>
  )
}

export function SettingsPage({ section, theme, onTheme }: { section: string; theme: ThemePref; onTheme: (t: ThemePref) => void }) {
  const me = useMe().data
  const s = SECTIONS.find((x) => x.id === section && (!x.when || x.when(me))) ?? SECTIONS[0]
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-8 px-8 pt-8 pb-16 max-[640px]:px-4">
      <header className="flex flex-col gap-1">
        <span className="text-[13px] text-t3">{tr('Настройки')} / {tr(s.group)}</span>
        <h1 className="text-2xl font-semibold tracking-[-.02em]">{tr(s.label)}</h1>
        <p className="text-sm text-t2">{tr(s.hint)}</p>
      </header>
      {s.id === 'profile' && <Profile />}
      {s.id === 'security' && me?.user && (
        <>
          <SettingsSection title={tr('Вход и пароль')}><div className="flex flex-col gap-4 p-5"><AccountSection user={me.user} /></div></SettingsSection>
          <SettingsSection title={tr('Двухфакторный вход и сессии')}><div className="p-5 [&>section]:border-t-0 [&>section]:pt-0"><AccountSecurity /></div></SettingsSection>
        </>
      )}
      {s.id === 'workspace' && <WorkspaceGeneral />}
      {s.id === 'workspaces' && <WorkspaceList />}
      {s.id === 'appearance' && <Appearance theme={theme} onTheme={onTheme} />}
      {s.id === 'app' && <AppInfo />}
      {s.id === 'backups' && <Backups />}
      {s.id === 'users' && me?.user && <SettingsSection title={tr('Сервер')}><div className="flex flex-col gap-4 p-5"><Users me={me.user} /></div></SettingsSection>}
    </div>
  )
}

function Profile() {
  const qc = useQueryClient()
  const profile = useProfile()
  const me = useMe().data
  const [name, setName] = useState(profile.data?.name ?? '')
  const save = useMutation({
    mutationFn: () => api.setProfile(name.trim()),
    onSuccess: () => Promise.all([qc.invalidateQueries({ queryKey: ['profile'] }), qc.invalidateQueries({ queryKey: ['me'] })]),
  })
  const dirty = name.trim() !== '' && name.trim() !== profile.data?.name
  return (
    <SettingsSection title={tr('Личные данные')}>
      <SettingsRow label={tr('Имя')} hint={tr('Видно рядом с вашими задачами и действиями в этом пространстве.')}>
        <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); if (dirty && !save.isPending) save.mutate() }}>
          <input className={textInput} maxLength={NAME_MAX} value={name} onChange={(e) => setName(e.target.value)} aria-label={tr('Имя')} />
          <button type="submit" className={btnPri} disabled={!dirty || save.isPending}>{tr('Сохранить')}</button>
        </form>
        {save.isError && <p className="text-[13px] text-rd" role="alert">{errText(save.error)}</p>}
        {save.isSuccess && !dirty && <p className="text-[13px] text-t2" role="status">{tr('Сохранено')}</p>}
      </SettingsRow>
      {me?.mode === 'selfhost' && (
        <>
          <SettingsRow label={tr('Фото')} hint={tr('PNG, JPEG, WebP или SVG до 2 МБ')}><ProfilePhoto name={name} /></SettingsRow>
          <SettingsRow label={tr('Логин')} hint={tr('Сменить пароль и выйти можно в разделе «Безопасность».')}>
            <span className="font-mono text-sm">{me.user?.username}</span>
          </SettingsRow>
        </>
      )}
    </SettingsSection>
  )
}

// A miniature of the app in each theme, so the choice is visual rather than a word.
const SWATCH = {
  light: { bg: '#ffffff', side: '#f8fafc', line: '#e2e8f0', ink: '#543099' },
  dark: { bg: '#0a0a0a', side: '#171717', line: '#2e2e2e', ink: '#ceb7ff' },
}
function Swatch({ kind }: { kind: 'light' | 'dark' }) {
  const c = SWATCH[kind]
  return (
    <span className="flex h-full w-full" style={{ background: c.bg }}>
      <span className="flex w-1/3 flex-col gap-1.5 p-2" style={{ background: c.side }}>
        <span className="h-1.5 w-3/4 rounded-full" style={{ background: c.ink }} />
        <span className="h-1.5 w-full rounded-full" style={{ background: c.line }} />
        <span className="h-1.5 w-2/3 rounded-full" style={{ background: c.line }} />
      </span>
      <span className="flex grow flex-col gap-1.5 p-2">
        <span className="h-2 w-1/2 rounded-full" style={{ background: c.line }} />
        <span className="h-6 w-full rounded-md border" style={{ borderColor: c.line }} />
        <span className="h-6 w-full rounded-md border" style={{ borderColor: c.line }} />
      </span>
    </span>
  )
}

const tile = 'flex flex-col gap-2 rounded-xl border border-ln bg-sf p-1.5 pb-2.5 text-left text-sm hover:border-ln2 aria-pressed:border-ink aria-pressed:ring-[3px] aria-pressed:ring-ink/20'

function Appearance({ theme, onTheme }: { theme: ThemePref; onTheme: (t: ThemePref) => void }) {
  const lang = useLang()
  return (
    <>
      <SettingsSection title={tr('Тема')} hint={tr('«Как в системе» следует за настройкой устройства.')}>
        <div className="grid grid-cols-3 gap-3 p-5 max-[520px]:grid-cols-1" role="group" aria-label={tr('Тема')}>
          {THEMES.map((t) => (
            <button key={t.id} type="button" className={tile} aria-pressed={theme === t.id} onClick={() => onTheme(t.id)}>
              <span className="relative block h-24 overflow-hidden rounded-lg border border-ln">
                {t.id === 'system'
                  ? <><Swatch kind="light" /><span className="absolute inset-0 [clip-path:polygon(100%_0,100%_100%,0_100%)]"><Swatch kind="dark" /></span></>
                  : <Swatch kind={t.id} />}
              </span>
              <span className="flex items-center gap-1.5 px-1 font-medium"><Icon name={t.icon} size={14} sw={2} />{tr(t.label)}
                {theme === t.id && <span className="ml-auto text-act"><Icon name="check" size={14} sw={2.4} /></span>}</span>
            </button>
          ))}
        </div>
      </SettingsSection>
      <SettingsSection title={tr('Язык')} hint={tr('Даты и числа тоже следуют выбранному языку.')}>
        <div className="grid grid-cols-3 gap-3 p-5 max-[520px]:grid-cols-1" role="group" aria-label={tr('Язык')}>
          {LANGS.map((l) => (
            <button key={l.id} type="button" lang={l.id} className={tile + ' flex-row items-center px-3 py-3'} aria-pressed={lang === l.id} onClick={() => setLang(l.id)}>
              <span className="grid size-8 place-items-center rounded-md bg-sk font-mono text-xs font-semibold">{l.id.toUpperCase()}</span>
              <span className="grow font-medium">{l.name}</span>
              {lang === l.id && <span className="text-act"><Icon name="check" size={14} sw={2.4} /></span>}
            </button>
          ))}
        </div>
      </SettingsSection>
    </>
  )
}

function AppInfo() {
  const me = useMe().data
  return (
    <>
      <SettingsSection title={tr('Это устройство')}>
        <SettingsRow label={tr('Установка')} hint={tr('Plandillo работает как отдельное приложение, без вкладки браузера.')}>
          <div><InstallPlandillo fallback={<span className="text-sm text-t2">{tr('Установка недоступна в этом браузере или уже выполнена.')}</span>} /></div>
        </SettingsRow>
      </SettingsSection>
      <SettingsSection title={tr('Сервер')}>
        <SettingsRow label={tr('Адрес')}><span className="font-mono text-sm">{location.host}</span></SettingsRow>
        <SettingsRow label={tr('Режим')} hint={me?.mode === 'selfhost' ? tr('Несколько пользователей, вход по паролю.') : tr('Один пользователь, без входа.')}>
          <span className="text-sm">{me?.mode === 'selfhost' ? tr('Свой сервер') : tr('Локально')}</span>
        </SettingsRow>
        <SettingsRow label={tr('Интеграции')} hint={tr('MCP, Code Mode, токен и доступ для агентов.')}>
          <div><Link to="/integrations" className={btn}><Icon name="plug" size={15} />{tr('Открыть интеграции')}</Link></div>
        </SettingsRow>
      </SettingsSection>
      <SettingsSection title={tr('Проект')}>
        <SettingsRow label={tr('Исходный код')}>
          <div><a className={btn} href="https://github.com/huntly-digital/plandillo" target="_blank" rel="noopener noreferrer"><Icon name="github" size={15} />GitHub</a></div>
        </SettingsRow>
        <SettingsRow label={tr('Нашли ошибку?')} hint={tr('Опишите, что произошло, и мы разберёмся.')}>
          <div><a className={btn} href="https://github.com/huntly-digital/plandillo/issues/new" target="_blank" rel="noopener noreferrer"><Icon name="flag" size={15} />{tr('Сообщить о проблеме')}</a></div>
        </SettingsRow>
      </SettingsSection>
    </>
  )
}

const HOSTING = 'https://github.com/huntly-digital/plandillo/blob/main/docs/hosting.md'
const megabytes = (bytes: number) => new Intl.NumberFormat(locale(), { style: 'unit', unit: 'megabyte', maximumFractionDigits: 1 }).format(bytes / 1e6)
const when = (iso: string) => new Date(iso).toLocaleString(locale(), { dateStyle: 'medium', timeStyle: 'short' })

function Backups() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['admin', 'backups'], queryFn: api.adminBackups })
  const create = useMutation({ mutationFn: api.createBackup, onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'backups'] }) })
  if (q.isPending) return <SettingsSection title={tr('Копии базы данных')}><SkeletonRows n={3} /></SettingsSection>
  if (q.isError) return <SettingsSection title={tr('Копии базы данных')}><ErrorState error={q.error} onRetry={() => q.refetch()} /></SettingsSection>
  if (!q.data.available)
    return (
      <SettingsSection title={tr('Копии базы данных')}>
        <SettingsRow label={tr('PostgreSQL')} hint={tr('Сервер хранит данные в PostgreSQL. Копии делает оператор базы: pg_dump или резервные копии провайдера.')}>
          <div><a className={btn} href={HOSTING + '#postgresql-storage'} target="_blank" rel="noopener noreferrer">{tr('Как настроить копии')}</a></div>
        </SettingsRow>
      </SettingsSection>
    )
  const list = q.data.backups
  const row = 'flex flex-wrap items-center gap-3 border-t border-ln px-5 py-3 first:border-t-0'
  return (
    <>
      <SettingsSection title={tr('Копии базы данных')}
        hint={tr('Сервер сохраняет копию при запуске и раз в день и хранит последние {n}. В копии все аккаунты и пространства сервера, поэтому храните её в надёжном месте.', { n: q.data.keep ?? 14 })}>
        <div className={row + ' bg-sf2 rounded-t-xl'}>
          <span className="grow text-sm text-t2">{list[0] ? tr('Последняя копия: {when}', { when: when(list[0].created_at) }) : tr('Копий пока нет')}</span>
          <button type="button" className={btnPri} disabled={create.isPending} onClick={() => create.mutate()}>
            <Icon name="plus" size={15} sw={2} />{create.isPending ? tr('Создаём копию…') : tr('Создать копию сейчас')}
          </button>
        </div>
        {create.isError && <p className="border-t border-ln px-5 py-3 text-[13px] text-rd" role="alert">{errText(create.error)}</p>}
        {list.map((b) => (
          <div key={b.name} className={row}>
            <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-sk text-t2"><Icon name="stack" size={15} /></span>
            <span className="flex min-w-0 grow flex-col leading-tight">
              <span className="text-sm font-medium">{when(b.created_at)}</span>
              <span className="truncate font-mono text-xs text-t3">{b.name} · {megabytes(b.size)}</span>
            </span>
            <a className={btn} href={'/api/admin/backups/' + encodeURIComponent(b.name)} download><Icon name="down" size={15} />{tr('Скачать')}</a>
          </div>
        ))}
      </SettingsSection>
      <SettingsSection title={tr('Восстановление')}>
        <SettingsRow label={tr('Как вернуть копию')} hint={tr('Восстанавливается весь сервер целиком: все аккаунты и пространства.')}>
          <ol className="flex list-decimal flex-col gap-1 pl-5 text-sm text-t2">
            <li>{tr('Остановите Plandillo.')}</li>
            <li>{tr('Замените файл базы (PLANDILLO_DB) скачанной копией.')}</li>
            <li>{tr('Запустите Plandillo снова.')}</li>
          </ol>
        </SettingsRow>
      </SettingsSection>
    </>
  )
}
