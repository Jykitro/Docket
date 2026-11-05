// Small shared pieces: popover menu, dialog, states, task badges.
import { type ReactNode, useEffect, useRef, useState } from 'react'
import { type Label, type Status, statusColor } from './api'
import { Icon } from './icons'

/** A button that opens a popover; closes on outside click and Esc. children gets a close() callback. */
export function Menu({ label, className = 'fc', right, ariaLabel, children }: {
  label: ReactNode; className?: string; right?: boolean; ariaLabel?: string; children: (close: () => void) => ReactNode
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
    <span className="popwrap" ref={ref}>
      <button type="button" className={className} aria-haspopup="menu" aria-expanded={open} aria-label={ariaLabel} onClick={() => setOpen(!open)}>
        {label}
      </button>
      {open && <div className={'pop' + (right ? ' right' : '')} role="menu">{children(() => setOpen(false))}</div>}
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
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog" role="dialog" aria-modal="true" aria-label={title} ref={trap} tabIndex={-1}>
        <h2>{title}</h2>
        {children}
      </div>
    </div>
  )
}

export function Skeleton({ w = '100%', h = 14 }: { w?: number | string; h?: number }) {
  return <span className="skeleton" style={{ display: 'block', width: w, height: h }} aria-hidden="true" />
}

export function SkeletonRows({ n = 4 }: { n?: number }) {
  return (
    <div role="status" aria-label="Загрузка" style={{ display: 'flex', flexDirection: 'column', gap: 14, padding: 16 }}>
      {Array.from({ length: n }, (_, i) => <Skeleton key={i} w={`${60 + ((i * 17) % 35)}%`} h={16} />)}
    </div>
  )
}

export function EmptyState({ icon, title, hint, action }: { icon: string; title: string; hint?: string; action?: ReactNode }) {
  return (
    <div className="empty">
      <span className="ic"><Icon name={icon} size={20} /></span>
      <b>{title}</b>
      {hint && <span>{hint}</span>}
      {action}
    </div>
  )
}

export function ErrorState({ message, onRetry }: { message?: string; onRetry: () => void }) {
  return (
    <div className="empty error" role="alert">
      <span className="ic"><Icon name="x" size={20} /></span>
      <b>Не удалось загрузить</b>
      <span>{message ?? 'Сервер не ответил'}</span>
      <button type="button" className="btn" onClick={onRetry}>Повторить</button>
    </div>
  )
}

/** An on/off switch (design: .tg). */
export function Toggle({ on, onChange, label, disabled }: { on: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean }) {
  return <button type="button" role="switch" aria-checked={on} aria-label={label} disabled={disabled} className={'tg' + (on ? ' on' : '')} onClick={() => onChange(!on)} />
}

/** A button that asks twice: the first click arms it for 4 seconds, the second runs the action. */
export function ConfirmButton({ label, armed, onConfirm, className = 'btn', icon, disabled, ariaLabel }: {
  label: string; armed: string; onConfirm: () => void; className?: string; icon?: ReactNode; disabled?: boolean; ariaLabel?: string
}) {
  const [on, setOn] = useState(false)
  useEffect(() => {
    if (!on) return
    const t = setTimeout(() => setOn(false), 4000)
    return () => clearTimeout(t)
  }, [on])
  return (
    <button type="button" className={className + (on ? ' warn' : '')} disabled={disabled} aria-label={on ? armed : ariaLabel} title={on ? armed : ariaLabel} onClick={() => (on ? (setOn(false), onConfirm()) : setOn(true))}>
      {icon}{on ? armed : label}
    </button>
  )
}

export const PriorityChip = ({ p }: { p: string }) => <span className={`pr ${p.toLowerCase()}`}>{p}</span>

export const ClaudeBadge = () => (
  <span className="claude" title="Создано Claude"><Icon name="spark" size={11} sw={2.2} />claude</span>
)

export function AreaDot({ area, labels }: { area: string; labels: Label[] }) {
  if (!area) return null
  const color = labels.find((l) => l.name === area)?.color ?? 'var(--bk)'
  return <span className="mc"><span className="dot" style={{ width: 7, height: 7, flexBasis: 7, background: color }} />{area}</span>
}

/** The coloured dot of a status column or chip. */
export const StatusDot = ({ s }: { s: Status }) => <span className="dot" style={{ width: 9, height: 9, flexBasis: 9, background: statusColor(s.color) }} />
