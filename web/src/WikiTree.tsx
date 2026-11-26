import { type DragEvent, type ReactNode, useCallback, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, errText, type Page, type Project } from './api'
import { locale, tr } from './i18n'
import { Icon } from './icons'
import { Link } from './router'
import { zoom } from './theme'
import { cap, MENUITEM } from './ui'

type Anchor = { id: number; shortcut: boolean; x: number; y: number; trigger: HTMLElement }
type Layout = Parameters<typeof api.setPageLayout>[1]
type Action = { icon: string; label: string; layout: Layout; disabled?: boolean }

function PageMenu({ anchor, title, actions, close, run }: {
  anchor: Anchor; title: string; actions: Action[]; close: () => void; run: (layout: Layout) => void
}) {
  const menu = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const el = menu.current!
    const box = el.getBoundingClientRect(), z = zoom() // anchor and box are on-screen pixels; left/top are zoomed again
    el.style.left = Math.max(4, Math.min(anchor.x, window.innerWidth - box.width - 4)) / z + 'px'
    el.style.top = Math.max(4, Math.min(anchor.y, window.innerHeight - box.height - 4)) / z + 'px'
    el.querySelector('button')?.focus({ preventScroll: true })
    const outside = (e: Event) => { if (!el.contains(e.target as Node)) close() }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('focusin', outside)
    document.addEventListener('scroll', outside, true)
    window.addEventListener('resize', close)
    return () => {
      document.removeEventListener('pointerdown', outside)
      document.removeEventListener('focusin', outside)
      document.removeEventListener('scroll', outside, true)
      window.removeEventListener('resize', close)
    }
  }, [anchor, close])
  return createPortal(
    <div ref={menu} role="menu" data-menu aria-label={title}
      className="fixed z-50 w-max max-w-[calc(100vw-8px)] max-h-[calc(100dvh-8px)] overflow-y-auto rounded-md border border-ln bg-sf p-1 shadow-pop"
      onKeyDown={(e) => {
        if (e.key === 'Escape' || e.key === 'Tab') {
          if (e.key === 'Escape') e.preventDefault()
          e.stopPropagation()
          close()
          anchor.trigger.focus({ preventScroll: true })
          return
        }
        const items = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')]
        const i = items.findIndex((el) => el === document.activeElement)
        const next = { ArrowDown: i + 1, ArrowUp: i - 1, Home: 0, End: items.length - 1 }[e.key]
        if (next !== undefined) {
          e.preventDefault()
          items[(next + items.length) % items.length].focus()
        }
      }}>
      {actions.map((a, i) => (
        <div key={a.icon} className={i === 1 ? 'mt-1 border-t border-ln pt-1' : undefined}>
          <button type="button" role="menuitem" disabled={a.disabled} className={MENUITEM + ' focus-visible:bg-sk disabled:opacity-40'}
            onClick={() => { close(); anchor.trigger.focus({ preventScroll: true }); run(a.layout) }}>
            <Icon name={a.icon} size={15} />{a.label}
          </button>
        </div>
      ))}
    </div>, document.body)
}

export function WikiTree({ pages, current, project, leave }: { pages: Page[]; current?: number; project: Project; leave: () => boolean }) {
  const qc = useQueryClient()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const closeMenu = useCallback(() => setMenu(null), [])
  const [dragged, setDragged] = useState<Page | null>(null)
  const [drop, setDrop] = useState<{ id: number; before: boolean } | null>(null)
  const layout = useMutation({
    mutationFn: ({ id, value }: { id: number; value: Layout }) => api.setPageLayout(id, value),
    onSettled: () => qc.invalidateQueries({ queryKey: ['pages', project.id] }),
  })

  // ListPages already orders by (position, id), including ties left by a deleted parent.
  const kids = new Map<number | null, Page[]>()
  const ids = new Set(pages.map((p) => p.id))
  for (const p of pages) {
    const parent = p.parent_id !== null && ids.has(p.parent_id) ? p.parent_id : null
    if (!kids.has(parent)) kids.set(parent, [])
    kids.get(parent)!.push(p)
  }
  const pins = pages.filter((p) => p.pinned).sort((a, b) => a.title.localeCompare(b.title, locale()))
  const selected = pages.find((p) => p.id === menu?.id)
  const siblings = selected ? kids.get(selected.parent_id) ?? [] : []
  const index = siblings.findIndex((p) => p.id === selected?.id)
  const actions: Action[] = selected ? [
    { icon: 'pin', label: selected.pinned ? tr('Открепить страницу') : tr('Закрепить страницу'), layout: { pinned: !selected.pinned } },
    { icon: 'top', label: tr('В начало раздела'), disabled: index <= 0, layout: { before_id: siblings[0]?.id } },
    { icon: 'up', label: tr('Переместить выше'), disabled: index <= 0, layout: { before_id: siblings[index - 1]?.id } },
    { icon: 'down', label: tr('Переместить ниже'), disabled: index < 0 || index >= siblings.length - 1, layout: { after_id: siblings[index + 1]?.id } },
  ] : []

  const finishDrag = () => { setDragged(null); setDrop(null) }
  const beforeRow = (e: DragEvent) => {
    const box = e.currentTarget.getBoundingClientRect()
    return e.clientY < box.top + box.height / 2
  }
  const row = (p: Page, depth: number, shortcut = false) => {
    const active = current === p.id
    const target = !shortcut && dragged && dragged.id !== p.id && dragged.parent_id === p.parent_id
    const line = target && drop?.id === p.id
    const label = tr('Действия со страницей «{title}»', { title: p.title })
    return <div className={'group relative flex min-h-8 items-center rounded-md hover:bg-sk focus-within:bg-sk ' + (active ? 'bg-sk text-tx' : 'text-t2')}
      onContextMenu={(e) => {
        e.preventDefault()
        if (!layout.isPending) setMenu({ id: p.id, shortcut, x: e.clientX, y: e.clientY, trigger: e.currentTarget.querySelector('button')! })
      }}
      onDragOver={(e) => {
        if (!target) return
        e.preventDefault(); e.dataTransfer.dropEffect = 'move'
        setDrop({ id: p.id, before: beforeRow(e) })
      }}
      onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node | null) && drop?.id === p.id) setDrop(null) }}
      onDrop={(e) => {
        if (!target || !dragged) return
        e.preventDefault()
        layout.mutate({ id: dragged.id, value: beforeRow(e) ? { before_id: p.id } : { after_id: p.id } })
        finishDrag()
      }}>
      {line && <span aria-hidden="true" className={'pointer-events-none absolute inset-x-1 h-0.5 bg-act ' + (drop.before ? 'top-0' : 'bottom-0')} />}
      <Link to={`/p/${project.slug}/wiki/${p.id}`} aria-current={active ? 'page' : undefined} title={p.title}
        className={'flex min-h-8 min-w-0 flex-1 items-center gap-2 rounded-md text-sm no-underline [&>svg]:shrink-0 ' + (active ? 'font-medium' : '')}
        style={{ paddingLeft: Math.min(8 + depth * 16, 88) }} onClick={(e) => { if (!leave()) e.preventDefault() }}
        draggable={!shortcut && !layout.isPending}
        onDragStart={(e) => {
          setMenu(null); setDragged(p)
          e.dataTransfer.effectAllowed = 'move'
          e.dataTransfer.setData('text/plain', p.title)
        }} onDragEnd={finishDrag}
        onKeyDown={(e) => {
          if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
            e.preventDefault()
            const trigger = e.currentTarget, box = trigger.getBoundingClientRect()
            if (!layout.isPending) setMenu({ id: p.id, shortcut, x: box.left, y: box.bottom, trigger })
          }
        }}>
        <Icon name={shortcut ? 'pin' : 'doc'} size={14} /><span className="truncate">{p.title}</span>
      </Link>
      <button type="button" aria-label={label} title={label} aria-haspopup="menu" aria-expanded={menu?.id === p.id && menu.shortcut === shortcut}
        disabled={layout.isPending}
        className="grid size-8 shrink-0 place-items-center rounded-md text-t3 hover:text-tx [@media(hover:hover)]:opacity-0 group-hover:opacity-100 group-focus-within:opacity-100"
        onClick={(e) => {
          const trigger = e.currentTarget, box = trigger.getBoundingClientRect()
          setMenu({ id: p.id, shortcut, x: box.left, y: box.bottom, trigger })
        }}><Icon name="more" size={16} sw={3} /></button>
    </div>
  }
  const level = (parent: number | null, depth: number): ReactNode => kids.get(parent)?.map((p) =>
    <li key={p.id}>{row(p, depth)}{kids.has(p.id) && <ul>{level(p.id, depth + 1)}</ul>}</li>)

  return <nav className="max-h-64 overflow-y-auto min-[900px]:@3xl:max-h-none" aria-label={tr('Страницы')}>
    {pins.length > 0 && <div className="mb-2 border-b border-ln pb-2">
      <h3 className={cap + ' mb-1 px-2'}>{tr('Закреплённые')}</h3>
      <ul aria-label={tr('Закреплённые')}>{pins.map((p) => <li key={p.id}>{row(p, 0, true)}</li>)}</ul>
    </div>}
    <ul aria-label={tr('Страницы')}>{level(null, 0)}</ul>
    {menu && selected && <PageMenu anchor={menu} title={selected.title} actions={actions} close={closeMenu}
      run={(value) => layout.mutate({ id: selected.id, value })} />}
    {layout.isError && <p role="alert" className="mt-2 rounded-md bg-rds p-2 text-sm text-rd">{errText(layout.error)}</p>}
  </nav>
}
