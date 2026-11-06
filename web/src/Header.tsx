// Top bar in the shadcn-admin layout: sidebar trigger, separator, search, then theme switch and profile on the right.
import { useProfile } from './api'
import { THEMES } from './AppSettings'
import { Icon } from './icons'
import { type ThemePref } from './theme'
import { MENUITEM, Menu, kbd } from './ui'

const ghost = 'inline-grid size-8 place-items-center rounded-md bg-transparent p-0 text-tx hover:bg-sk'

export function Header({ onToggle, onSearch, theme, resolved, onTheme, onSettings }: {
  onToggle: () => void; onSearch: () => void; theme: ThemePref; resolved: 'light' | 'dark'; onTheme: (t: ThemePref) => void; onSettings: () => void
}) {
  const name = useProfile().data?.name ?? ''
  return (
    <header className="sticky top-0 z-30 flex h-16 shrink-0 items-center gap-3 border-b border-ln bg-bg/80 px-4 backdrop-blur-lg sm:gap-4">
      <button type="button" className="inline-grid size-7 place-items-center rounded-md border border-ln bg-sf p-0 text-tx shadow-xs hover:bg-sk max-md:scale-125" aria-label="Боковая панель" onClick={onToggle}>
        <Icon name="panelL" size={16} />
      </button>
      <span className="h-6 w-px bg-ln" role="separator" aria-orientation="vertical" />
      <button type="button" className="inline-flex h-9 w-40 items-center gap-2 rounded-md border border-ln bg-sk/25 px-3 text-sm text-t3 shadow-xs hover:bg-sk md:w-64" onClick={onSearch}>
        <Icon name="search" size={15} /><span className="grow text-left">Поиск</span><span className={kbd + ' max-sm:hidden'}>⌘K</span>
      </button>
      <div className="ml-auto flex items-center gap-2">
        <Menu right ariaLabel="Тема" className={ghost} label={<Icon name={resolved === 'dark' ? 'moon' : 'sun'} size={17} />}>
          {(close) => THEMES.map((t) => (
            <button key={t.id} type="button" className={MENUITEM} onClick={() => { onTheme(t.id); close() }}>
              <Icon name={t.icon} size={15} />{t.label}{theme === t.id && <span className="ml-auto"><Icon name="check" size={14} sw={2.2} /></span>}
            </button>
          ))}
        </Menu>
        <button type="button" className="grid size-8 place-items-center rounded-full bg-sk text-sm font-semibold hover:bg-ln2" aria-label="Профиль и настройки" onClick={onSettings}>
          {name ? name.charAt(0).toUpperCase() : <Icon name="user" size={15} sw={2} />}
        </button>
      </div>
    </header>
  )
}
