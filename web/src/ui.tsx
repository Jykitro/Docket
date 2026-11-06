// Small shared pieces: popover menu, dialog, states, task badges.
import { type ReactNode, useEffect, useRef, useState } from 'react'
import { type Label } from './api'
import { Icon } from './icons'

// Shared utility strings (legacy .btn, .btn.pri, .btn.sq, .btn.warn, .kbd, .cap, .mc, .dot).
const btnBase = 'inline-flex items-center gap-2 h-9 rounded-md border text-sm font-medium no-underline whitespace-nowrap disabled:opacity-50 disabled:cursor-default'
export const btn = `${btnBase} px-3 border-ln bg-sf text-tx shadow-xs hover:bg-sk`
export const btnPri = `${btnBase} px-3 border-ink bg-ink text-inkt shadow-xs hover:bg-ink/90`
export const btnSq = `${btnBase} w-9 p-0 justify-center border-ln bg-sf text-tx shadow-xs hover:bg-sk`
export const btnWarn = `${btnBase} px-3 border-rd bg-sf text-rd shadow-xs hover:bg-sk`
const kbdBase = 'font-mono text-[11px] font-medium leading-[normal] rounded-sm px-1.5 py-px border'
export const kbd = `${kbdBase} text-t3 border-ln bg-sk`
export const kbdPri = `${kbdBase} bg-transparent text-inkt border-current opacity-70`
export const cap = 'text-xs font-medium text-t3'
export const mc = 'inline-flex items-center gap-[5px] text-[12px] text-t3'
// shadcn-admin's faceted filter: a small dashed outline button
const fc = 'inline-flex items-center gap-1.5 h-8 px-3 rounded-md border border-dashed border-ln bg-sf text-xs font-medium text-t2 whitespace-nowrap hover:bg-sk'

/** An item button inside a popover menu. */
export const MENUITEM = 'flex h-8 w-full items-center gap-2 rounded-sm bg-transparent px-2 text-left text-sm hover:bg-sk'

/** A button that opens a popover; closes on outside click and Esc. children gets a close() callback. */
export function Menu({ label, className = fc, right, up, ariaLabel, children }: {
  label: ReactNode; className?: string; right?: boolean; up?: boolean; ariaLabel?: string; children: (close: () => void) => ReactNode
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    if (!open) return
    const down = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    const key = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', down)
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('mousedown', down)
      document.removeEventListener('keydown', key)
    }
  }, [open])
  return (
    <span className="relative inline-block" ref={ref}>
      <button type="button" className={className} aria-haspopup="menu" aria-expanded={open} aria-label={ariaLabel} onClick={() => setOpen(!open)}>
        {label}
      </button>
      {open && <div data-menu className={'absolute z-30 min-w-[200px] max-h-80 overflow-y-auto bg-sf border border-ln rounded-md shadow-pop p-1 ' + (up ? 'bottom-[calc(100%+6px)] ' : 'top-[calc(100%+6px)] ') + (right ? 'right-0' : 'left-0')} role="menu">{children(() => setOpen(false))}</div>}
    </span>
  )
}

const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'

/** Keeps Tab inside a dialog and gives focus back to what opened it when it closes. Put the ref on a tabIndex={-1} element. */
export function useFocusTrap<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  useEffect(() => {
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
      if (prev && document.contains(prev)) prev.focus()
    }
  }, [])
  return ref
}

export function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  useEffect(() => {
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', key)
    return () => document.removeEventListener('keydown', key)
  }, [onClose])
  const trap = useFocusTrap<HTMLDivElement>()
  return (
    <div className="fixed inset-0 z-50 bg-black/50 flex items-start justify-center pt-[12vh] px-4 pb-4 overflow-y-auto" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="w-full max-w-[560px] bg-sf border border-ln rounded-lg shadow-pop p-6 flex flex-col gap-4" role="dialog" aria-modal="true" aria-label={title} ref={trap} tabIndex={-1}>
        <h2 className="text-lg font-semibold leading-none tracking-tight">{title}</h2>
        {children}
      </div>
    </div>
  )
}

export function Skeleton({ w = '100%', h = 14 }: { w?: number | string; h?: number }) {
  return <span className="block rounded-md bg-sk animate-pulse" style={{ width: w, height: h }} aria-hidden="true" />
}

export function SkeletonRows({ n = 4 }: { n?: number }) {
  return (
    <div role="status" aria-label="Загрузка" className="flex flex-col gap-3.5 p-4">
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

export function ErrorState({ message, onRetry }: { message?: string; onRetry: () => void }) {
  return (
    <div className={emptyBox} role="alert">
      <span className={emptyIc + 'bg-rds text-rd'}><Icon name="x" size={20} /></span>
      <b className="text-tx text-[15px]">Не удалось загрузить</b>
      <span>{message ?? 'Сервер не ответил'}</span>
      <button type="button" className={btn} onClick={onRetry}>Повторить</button>
    </div>
  )
}

// shadcn Switch: 32x18.4px track, 16px thumb
const tg = 'block relative w-8 h-[1.15rem] flex-[0_0_32px] rounded-full p-0 border border-transparent shadow-xs bg-ln2 aria-checked:bg-ink disabled:opacity-50 disabled:cursor-default '
  + "after:content-[''] after:absolute after:top-px after:left-px after:size-4 after:rounded-full after:bg-bg after:transition-[left] after:duration-[120ms] after:ease-[ease] aria-checked:after:left-[calc(100%-17px)]"

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

export const ClaudeBadge = () => (
  <span className="inline-flex items-center gap-1 text-[11px] font-medium text-act bg-acs rounded-[5px] py-0.5 pr-1.5 pl-[5px]" title="Создано Claude"><Icon name="spark" size={11} sw={2.2} />claude</span>
)

export function AreaDot({ area, labels }: { area: string; labels: Label[] }) {
  if (!area) return null
  const color = labels.find((l) => l.name === area)?.color ?? 'var(--bk)'
  return <span className={mc}><span className="inline-block w-[7px] h-[7px] rounded-full flex-[0_0_7px]" style={{ background: color }} />{area}</span>
}
