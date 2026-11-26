// Small shared pieces: popover menu, dialog, states, task badges.
import { type ReactNode, useEffect, useRef, useState } from 'react'
import { useOnline } from './connection'
import { ApiError, type ErrorKind, agentName, errText, errorKind, type Label } from './api'
import { Icon } from './icons'
import { LANGS, setLang, tr, useLang } from './i18n'
import { Mascot } from './Mascot'
import { workspacePath } from './workspacePath'

// Shared utility strings (legacy .btn, .btn.pri, .btn.sq, .btn.warn, .kbd, .cap, .mc, .dot).
const btnBase = 'inline-flex items-center gap-2 h-9 rounded-md border text-sm font-medium no-underline whitespace-nowrap disabled:opacity-50 disabled:cursor-default'
export const btn = `${btnBase} px-3 border-ln bg-sf text-tx shadow-xs hover:bg-sk`
export const btnPri = `${btnBase} px-3 border-ink bg-ink text-inkt shadow-xs hover:bg-ink/90`
export const btnSq = `${btnBase} w-9 p-0 justify-center border-ln bg-sf text-tx shadow-xs hover:bg-sk`
const btnWarn = `${btnBase} px-3 border-rd bg-sf text-rd shadow-xs hover:bg-sk`
const kbdBase = 'font-mono text-[11px] font-medium leading-[normal] rounded-sm px-1.5 py-px border'
export const kbd = `${kbdBase} text-t3 border-ln bg-sk`
export const kbdPri = `${kbdBase} bg-transparent text-inkt border-current opacity-70`
export const cap = 'text-xs font-medium text-t3'
export const textInput = 'h-9 w-full min-w-0 rounded-md border border-ln bg-sf px-2.5 text-sm text-tx shadow-xs focus:border-ring focus:ring-[3px] focus:ring-ring/30 focus:outline-none'
export const MENUSEP = 'my-1 h-px bg-ln'

/** The user's photo, or the first letter of their name on a soft tile. */
export function Avatar({ name, photo, size = 32 }: { name?: string; photo?: string | null; size?: number }) {
  const box = { width: size, height: size, fontSize: Math.round(size * 0.42) }
  if (photo) return <img src={photo} style={box} className="shrink-0 rounded-full object-cover" alt="" />
  return <span style={box} className="grid shrink-0 place-items-center rounded-full bg-acs font-semibold text-act">{name ? name.charAt(0).toUpperCase() : <Icon name="user" size={Math.round(size * 0.47)} sw={2} />}</span>
}

/** A titled block of a settings page: heading and hint on top, a card of rows below. */
export function SettingsSection({ title, hint, danger, children }: { title: string; hint?: string; danger?: boolean; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <h2 className={'text-[15px] font-semibold ' + (danger ? 'text-rd' : '')}>{title}</h2>
        {hint && <p className="text-[13px] text-t2">{hint}</p>}
      </div>
      <div className={'flex flex-col rounded-xl border bg-sf ' + (danger ? 'border-rd/40' : 'border-ln')}>{children}</div>
    </section>
  )
}

/** One row of a settings card: what it is on the left, the control on the right (stacked on narrow screens). */
export function SettingsRow({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="grid gap-3 border-t border-ln px-5 py-4 first:border-t-0 md:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] md:items-center">
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium">{label}</span>
        {hint && <span className="text-[13px] text-t3">{hint}</span>}
      </div>
      <div className="flex min-w-0 flex-col gap-2">{children}</div>
    </div>
  )
}
export const mc = 'inline-flex items-center gap-[5px] text-[12px] text-t3'
// shadcn-admin's faceted filter: a small dashed outline button
const fc = 'inline-flex items-center gap-1.5 h-8 px-3 rounded-md border border-dashed border-ln bg-sf text-xs font-medium text-t2 whitespace-nowrap hover:bg-sk'

/** An item button inside a popover menu. */
export const MENUITEM = 'flex h-8 w-full items-center gap-2 rounded-sm bg-transparent px-2 text-left text-sm hover:bg-sk'

/** A button that opens a popover; closes on outside click and Esc. children gets a close() callback. */
export function Menu({ label, className = fc, right, up, block, ariaLabel, title, disabled, children }: {
  label: ReactNode; className?: string; right?: boolean; up?: boolean; block?: boolean; ariaLabel?: string; title?: string; disabled?: boolean; children: (close: () => void) => ReactNode
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLSpanElement>(null)
  // Closing from inside the list gives focus back to the trigger instead of dropping it on <body>.
  const close = () => { setOpen(false); ref.current?.querySelector('button')?.focus() }
  useEffect(() => {
    if (!open) return
    const down = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { setOpen(false); ref.current?.querySelector('button')?.focus() }
      if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
      // Arrow keys walk the items, starting from the trigger.
      const items = [...(ref.current?.querySelectorAll<HTMLElement>('[data-menu] button:not(:disabled), [data-menu] a[href]') ?? [])]
      if (!items.length) return
      e.preventDefault()
      const i = items.indexOf(document.activeElement as HTMLElement), down = e.key === 'ArrowDown'
      items[i < 0 ? (down ? 0 : items.length - 1) : (i + (down ? 1 : -1) + items.length) % items.length].focus()
    }
    document.addEventListener('mousedown', down)
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('mousedown', down)
      document.removeEventListener('keydown', key)
    }
  }, [open])
  return (
    <span className={'relative ' + (block ? 'block' : 'inline-block')} ref={ref}>
      <button type="button" className={className} aria-haspopup="menu" aria-expanded={open} aria-label={ariaLabel} title={title} disabled={disabled} onClick={() => setOpen(!open)}>
        {label}
      </button>
      {open && <div data-menu className={'absolute z-30 min-w-[200px] max-h-80 overflow-y-auto bg-sf border border-ln rounded-md shadow-pop p-1 ' + (up ? 'bottom-[calc(100%+6px)] ' : 'top-[calc(100%+6px)] ') + (right ? 'right-0' : 'left-0')} role="menu">{children(close)}</div>}
    </span>
  )
}

const selectBtn = 'inline-flex h-8 max-w-full items-center gap-1.5 rounded-md border border-ln bg-sf px-2.5 text-[13px] text-tx shadow-xs hover:bg-sk aria-expanded:border-ring disabled:opacity-50 '

/** A dropdown in place of the native <select>: the chosen option on a button, the list in a popover. showLabel also prints the label on the button. */
export function Select<T extends string>({ value, options, onChange, label, showLabel, disabled, block, right }: {
  value: T; options: readonly { value: T; label: string }[]; onChange: (v: T) => void; label: string; showLabel?: boolean; disabled?: boolean; block?: boolean; right?: boolean
}) {
  const current = options.find((o) => o.value === value)
  return (
    <Menu block={block} right={right} disabled={disabled} ariaLabel={label + (current ? ': ' + current.label : '')} className={selectBtn + (block ? 'w-full' : '')}
      label={<>{showLabel && <span className="shrink-0 text-t3">{label}</span>}<span className="min-w-0 grow truncate text-left font-medium">{current?.label ?? '—'}</span><span className="text-t3"><Icon name="chevron" size={15} /></span></>}>
      {(close) => options.map((o) => (
        <button key={o.value} type="button" role="menuitemradio" aria-checked={o.value === value} className={MENUITEM} onClick={() => { onChange(o.value); close() }}>
          <span className="grow truncate">{o.label}</span>{o.value === value && <Icon name="check" size={14} sw={2.2} />}
        </button>
      ))}
    </Menu>
  )
}

const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]),textarea:not([disabled]),summary,[tabindex]:not([tabindex="-1"])'

/** Keeps Tab inside a dialog and gives focus back to what opened it when it closes. Put the ref on a tabIndex={-1} element. */
export function useFocusTrap<T extends HTMLElement>(enabled = true) {
  const ref = useRef<T>(null)
  useEffect(() => {
    if (!enabled) return
    const prev = document.activeElement as HTMLElement | null
    const el = ref.current
    if (el && !el.contains(document.activeElement)) el.focus()
    const key = (e: KeyboardEvent) => {
      if (e.key !== 'Tab' || !el || !el.contains(document.activeElement)) return
      const f = [...el.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((x) => x.offsetParent !== null)
      if (!f.length) return
      const first = f[0], last = f[f.length - 1]
      if (e.shiftKey && (document.activeElement === first || document.activeElement === el)) { e.preventDefault(); last.focus() }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('keydown', key)
      if (prev && document.contains(prev) && (el?.contains(document.activeElement) || document.activeElement === document.body)) prev.focus({ preventScroll: true })
    }
  }, [enabled])
  return ref
}

/** A modal: centred, or a panel on the right edge with `side`. Esc and a click on the backdrop close it. */
export function Dialog({ title, onClose, side, children }: { title: string; onClose: () => void; side?: boolean; children: ReactNode }) {
  useEffect(() => {
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', key)
    return () => document.removeEventListener('keydown', key)
  }, [onClose])
  const trap = useFocusTrap<HTMLDivElement>()
  return (
    <div className={'fixed inset-0 z-50 bg-black/50 flex ' + (side ? 'justify-end' : 'items-start justify-center pt-[12vh] px-4 pb-4 overflow-y-auto')} onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={side ? 'h-full w-full max-w-[420px] overflow-y-auto bg-sf border-l border-ln shadow-pop p-6 flex flex-col gap-4' : 'w-full max-w-[560px] bg-sf border border-ln rounded-lg shadow-pop p-6 flex flex-col gap-4'} role="dialog" aria-modal="true" aria-label={title} ref={trap} tabIndex={-1}>
        <h2 className="text-lg font-semibold leading-none tracking-tight">{title}</h2>
        {children}
      </div>
    </div>
  )
}

export function Skeleton({ w = '100%', h = 14 }: { w?: number | string; h?: number }) {
  return <span className="block rounded-md bg-sk motion-safe:animate-pulse" style={{ width: w, height: h }} aria-hidden="true" />
}

export function SkeletonRows({ n = 4 }: { n?: number }) {
  const online = useOnline()
  if (!online) return <p role="status" className="p-4 text-sm text-t3">{tr('Недоступно офлайн: откройте эту страницу, когда будет связь')}</p>
  return (
    <div role="status" aria-label={tr('Загрузка')} className="flex flex-col gap-3.5 p-4">
      {Array.from({ length: n }, (_, i) => <Skeleton key={i} w={`${60 + ((i * 17) % 35)}%`} h={16} />)}
    </div>
  )
}

const emptyBox = 'flex flex-col items-center gap-2 text-center py-10 px-5 text-t3'
const emptyIc = 'size-10 rounded-xl grid place-items-center '

export function EmptyState({ icon, title, hint, action }: { icon: string; title: string; hint?: string; action?: ReactNode }) {
  return (
    <div className={emptyBox}>
      <span className={emptyIc + 'bg-sk text-t2'}><Icon name={icon} size={20} /></span>
      <b className="text-tx text-[15px]">{title}</b>
      {hint && <span>{hint}</span>}
      {action}
    </div>
  )
}

const ISSUES = 'https://github.com/huntly-digital/plandillo/issues/new'

/** Plain-language title and explanation for each kind of failure. */
const KINDS: Record<ErrorKind, { title: string; hint: string }> = {
  offline: { title: 'Нет связи с сервером', hint: 'Проверьте интернет. Если Plandillo запущен у вас, убедитесь, что сервер работает.' },
  auth: { title: 'Сессия закончилась', hint: 'Войдите снова, чтобы продолжить с того же места.' },
  forbidden: { title: 'Нет доступа', hint: 'У вашей учётной записи нет прав на это. Попросите доступ у владельца пространства.' },
  not_found: { title: 'Ничего не нашлось', hint: 'Возможно, это удалили или переместили. Проверьте адрес или вернитесь на главную.' },
  rate: { title: 'Слишком много запросов', hint: 'Сервер просит немного подождать. Повторите через минуту.' },
  server: { title: 'Сбой на сервере', hint: 'Дело не в вас. Повторите через минуту, а если не поможет, сообщите о проблеме.' },
  other: { title: 'Не удалось загрузить', hint: '' },
}

/** What a reader can quote when reporting it: the HTTP status and the server's error code. */
const errorCode = (e: unknown) => e instanceof ApiError && e.status ? `HTTP ${e.status}${e.code && e.code !== 'internal' ? ' · ' + e.code : ''}` : ''

/** Shared body of ErrorState and ErrorPage: armadillo scene, title, explanation, actions, and the code to quote. */
function ErrorBody({ kind, title, hint, code, actions, large }: { kind: ErrorKind; title?: string; hint?: string; code?: string; actions: ReactNode; large?: boolean }) {
  const k = KINDS[kind]
  return (
    <div className={'flex flex-col items-center gap-3 text-center ' + (large ? 'max-w-md' : 'max-w-sm px-5 py-10')}>
      <div className={large ? 'mb-2' : ''}><Mascot kind={kind} large={large} /></div>
      {large ? <h1 className="text-2xl font-semibold tracking-[-.02em]">{title ?? tr(k.title)}</h1> : <b className="text-[15px] text-tx">{title ?? tr(k.title)}</b>}
      <p className={'text-t2 ' + (large ? 'text-[15px]' : 'text-sm')}>{hint ?? tr(k.hint)}</p>
      <div className="mt-1 flex flex-wrap justify-center gap-2">{actions}</div>
      {code && <span className="font-mono text-[11px] text-t3">{code}</span>}
    </div>
  )
}

function errorActions(kind: ErrorKind, onRetry?: () => void) {
  if (kind === 'auth') return <button type="button" className={btnPri} onClick={() => location.reload()}>{tr('Войти снова')}</button>
  return (
    <>
      {onRetry && kind !== 'not_found' && <button type="button" className={btn} onClick={onRetry}>{tr('Повторить')}</button>}
      {kind === 'not_found' && <a className={btn} href={workspacePath('/')}>{tr('На главную')}</a>}
      {kind === 'server' && <a className={btn} href={ISSUES} target="_blank" rel="noopener noreferrer">{tr('Сообщить о проблеме')}</a>}
    </>
  )
}

/** A failed load inside a page: says what went wrong in plain words and offers the next step. */
export function ErrorState({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  const kind = errorKind(error)
  return (
    <div className="flex justify-center" role="alert">
      <ErrorBody kind={kind} hint={kind === 'other' ? errText(error) : undefined} code={errorCode(error)} actions={errorActions(kind, onRetry)} />
    </div>
  )
}

/** A whole screen for a failure: an unknown address, an unreachable server, a workspace that failed to load. */
export function ErrorPage({ kind, error, title, hint, actions, full, children }: {
  kind?: ErrorKind; error?: unknown; title?: string; hint?: string; actions?: ReactNode; full?: boolean; children?: ReactNode
}) {
  const k = kind ?? errorKind(error)
  return (
    <main className={'flex flex-col items-center justify-center px-6 py-16 ' + (full ? 'min-h-dvh bg-bg text-tx' : 'grow')} role="alert">
      <ErrorBody large kind={k} title={title} hint={hint ?? (k === 'other' && error ? errText(error) : undefined)} code={errorCode(error)}
        actions={actions ?? errorActions(k, error ? () => location.reload() : undefined)} />
      {children}
    </main>
  )
}

// shadcn Switch: 32x18.4px track, 16px thumb
const tg = 'block relative w-8 h-[1.15rem] flex-[0_0_32px] rounded-full p-0 border border-transparent shadow-xs bg-ln2 aria-checked:bg-ink disabled:opacity-50 disabled:cursor-default '
  + "after:content-[''] after:absolute after:top-px after:left-px after:size-4 after:rounded-full after:bg-bg motion-safe:after:transition-[left] motion-safe:after:duration-[120ms] after:ease-[ease] aria-checked:after:left-[calc(100%-17px)]"

/** An on/off switch. */
export function Toggle({ on, onChange, label, disabled }: { on: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean }) {
  return <button type="button" role="switch" aria-checked={on} aria-label={label} disabled={disabled} className={tg} onClick={() => onChange(!on)} />
}

/** A button that asks twice: the first click arms it for 4 seconds, the second runs the action. */
export function ConfirmButton({ label, armed, onConfirm, className, icon, disabled, ariaLabel }: {
  label: string; armed: string; onConfirm: () => void; className?: string; icon?: ReactNode; disabled?: boolean; ariaLabel?: string
}) {
  const [on, setOn] = useState(false)
  useEffect(() => {
    if (!on) return
    const t = setTimeout(() => setOn(false), 4000)
    return () => clearTimeout(t)
  }, [on])
  return (
    <button type="button" className={className ? className + (on ? ' warn' : '') : on ? btnWarn : btn} disabled={disabled} aria-label={on ? armed : ariaLabel} title={on ? armed : ariaLabel} onClick={() => (on ? (setOn(false), onConfirm()) : setOn(true))}>
      {icon}{on ? armed : label}
    </button>
  )
}

const PRIO: Record<string, string> = {
  p0: 'px-1.5 py-0.5 bg-rds text-rd',
  p1: 'px-1.5 py-0.5 bg-sk text-t2',
  p2: 'px-[5px] py-px border border-ln text-t3',
}
export const PriorityChip = ({ p }: { p: string }) => <span className={'font-mono text-[11px] font-semibold leading-[normal] rounded-[5px] ' + (PRIO[p.toLowerCase()] ?? '')}>{p}</span>

export const AgentBadge = ({ actor }: { actor: string }) => (
  <span className="inline-flex min-w-0 max-w-36 items-center gap-1 text-[11px] font-medium text-act bg-acs rounded-[5px] py-0.5 pr-1.5 pl-[5px]" title={tr('Автор: {name}', { name: agentName(actor) })}><Icon name="spark" size={11} sw={2.2} /><span className="truncate">{agentName(actor)}</span></span>
)

/** EN / RU / UK switch. It subscribes itself, so it works on the login screen too. */
export function LangSwitch() {
  const lang = useLang()
  return (
    <span className="inline-flex gap-0.5 rounded-md border border-ln p-0.5 text-xs" role="group" aria-label={tr('Язык')}>
      {LANGS.map((l) => (
        <button key={l.id} type="button" lang={l.id} title={l.name} aria-pressed={lang === l.id} onClick={() => setLang(l.id)}
          className="rounded-sm px-2 py-0.5 font-medium text-t3 aria-pressed:bg-sk aria-pressed:font-semibold aria-pressed:text-tx">{l.id.toUpperCase()}</button>
      ))}
    </span>
  )
}

export function AreaDot({ area, labels }: { area: string; labels: Label[] }) {
  if (!area) return null
  const color = labels.find((l) => l.name === area)?.color ?? 'var(--bk)'
  return <span className={mc}><span className="inline-block w-[7px] h-[7px] rounded-full flex-[0_0_7px]" style={{ background: color }} />{area}</span>
}
