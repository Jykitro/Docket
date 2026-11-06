// Project wiki: the page tree on the left (stacked above on narrow screens), a page or its editor on the right.
// Pages link to each other with [[Title]] and to tasks by code; backlinks and mentioned tasks sit under the text.
import { type KeyboardEvent, type MouseEvent, type ReactNode, type RefObject, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, type Page, type PageDetail, type Project, type TaskRef, api, errText, fmtStamp } from './api'
import { Icon } from './icons'
import { tr } from './i18n'
import { MD_PROSE, renderWiki } from './markdown'
import { Link, hrefWith, navigate, setParam } from './router'
import { ConfirmButton, EmptyState, ErrorState, Skeleton, SkeletonRows, btn, btnPri, cap, mc } from './ui'

const BOX = 'bg-sf border border-ln rounded-xl'
const SEG = 'inline-flex self-start gap-0.5 rounded-md border border-ln p-0.5 text-[12.5px]'
const SEGBTN = 'rounded-md px-2.5 py-[3px] text-t3 aria-pressed:bg-sk aria-pressed:font-semibold aria-pressed:text-tx'
const ROW = 'flex items-center gap-2 min-h-8 rounded-md pr-2 text-sm no-underline text-t2 hover:bg-sk'

type Ctx = { project: Project; pages: Page[]; tasks: TaskRef[]; leave: () => boolean; createByTitle: (title: string) => void }

/** Rendered wiki markdown; clicks on its [[links]] and task chips are handled here by delegation. */
function Body({ md, c }: { md: string; c: Ctx }) {
  const html = useMemo(() => renderWiki(md, { slug: c.project.slug, pages: c.pages, tasks: c.tasks }), [md, c.project.slug, c.pages, c.tasks])
  const click = (e: MouseEvent) => {
    const a = (e.target as HTMLElement).closest('a')
    if (!a || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey) return
    const { wiki, wikiNew, task } = a.dataset
    if (wiki) { e.preventDefault(); if (c.leave()) navigate(`/p/${c.project.slug}/wiki/${wiki}`) }
    else if (task) { e.preventDefault(); setParam('task', task, false) }
    else if (wikiNew !== undefined) { e.preventDefault(); if (c.leave()) c.createByTitle(wikiNew) }
  }
  return <div className={MD_PROSE} onClick={click} dangerouslySetInnerHTML={{ __html: html }} />
}

function Tree({ pages, current, slug, leave }: { pages: Page[]; current?: number; slug: string; leave: () => boolean }) {
  const kids = new Map<number | null, Page[]>()
  const ids = new Set(pages.map((p) => p.id))
  for (const p of [...pages].sort((a, b) => a.position - b.position || a.title.localeCompare(b.title))) {
    const k = p.parent_id !== null && ids.has(p.parent_id) ? p.parent_id : null
    kids.set(k, [...(kids.get(k) ?? []), p])
  }
  const level = (parent: number | null, depth: number): ReactNode =>
    kids.get(parent)?.map((p) => (
      <li key={p.id}>
        <Link to={`/p/${slug}/wiki/${p.id}`} onClick={(e) => { if (!leave()) e.preventDefault() }} aria-current={p.id === current ? 'page' : undefined}
          className={ROW + (p.id === current ? ' bg-sk font-medium text-tx' : '')} style={{ paddingLeft: 8 + depth * 16 }}>
          <Icon name="doc" size={14} /><span className="min-w-0 truncate">{p.title}</span>
        </Link>
        {kids.has(p.id) && <ul>{level(p.id, depth + 1)}</ul>}
      </li>
    ))
  return <ul aria-label={tr('Страницы')}>{level(null, 0)}</ul>
}

function Block({ title, children }: { title: string; children: ReactNode }) {
  return <section className="border-t border-ln pt-4 flex flex-col gap-2"><h3 className="text-[13px] font-semibold text-t2">{title}</h3><ul className="flex flex-col gap-1 text-sm">{children}</ul></section>
}

function PageEditor({ page, c, dirty, onDone }: { page: Page; c: Ctx; dirty: RefObject<boolean>; onDone: () => void }) {
  const qc = useQueryClient()
  const [orig, setOrig] = useState(page) // the version the draft started from; its updated_at is sent as expected_updated_at
  const [title, setTitle] = useState(page.title)
  const [body, setBody] = useState(page.body_md)
  const [preview, setPreview] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [error, setError] = useState('')
  const changed = title.trim() !== orig.title || body !== orig.body_md

  useEffect(() => {
    dirty.current = changed
    if (!changed) return
    const warn = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [changed, dirty])
  useEffect(() => () => { dirty.current = false }, [dirty])

  const load = (p: Page) => { setOrig(p); setTitle(p.title); setBody(p.body_md); setConflict(false); setError('') }
  const save = useMutation({
    mutationFn: async (v: { close: boolean; over?: boolean }) => {
      const expected = v.over ? (await api.page(page.id)).updated_at : orig.updated_at // "save over": take the fresh stamp
      return api.savePage(page.id, {
        ...(title.trim() !== orig.title && { title }), ...(body !== orig.body_md && { body_md: body }), expected_updated_at: expected,
      })
    },
    onSuccess: (p, v) => {
      load(p)
      qc.setQueryData<PageDetail>(['page', page.id], (old) => old && { ...old, ...p })
      qc.invalidateQueries()
      if (v.close) onDone()
    },
    onError: (e) => (e instanceof ApiError && e.code === 'conflict' ? setConflict(true) : setError(errText(e))),
  })
  const reload = useMutation({ mutationFn: () => api.page(page.id), onSuccess: load, onError: (e) => setError(errText(e)) })
  const busy = save.isPending || reload.isPending
  const submit = (close: boolean) => (changed ? save.mutate({ close }) : close && onDone())
  const keys = (e: KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && (e.key.toLowerCase() === 's' || e.code === 'KeyS')) { e.preventDefault(); if (!busy) submit(false) }
  }

  return (
    <div className="flex flex-col gap-4 p-5 max-[640px]:p-4" onKeyDown={keys}>
      {conflict && (
        <div role="alert" className="flex flex-col gap-2.5 rounded-lg border border-rd bg-rds p-3 text-sm">
          <span>{tr('Страница изменена в другом месте. Загрузите новую версию или сохраните свою поверх неё.')}</span>
          <span className="flex flex-wrap gap-2">
            <button type="button" className={btn} disabled={busy} onClick={() => reload.mutate()}>{tr('Загрузить новую')}</button>
            <button type="button" className={btn} disabled={busy} onClick={() => save.mutate({ close: false, over: true })}>{tr('Сохранить поверх')}</button>
          </span>
        </div>
      )}
      {error && <p role="alert" className="rounded-lg bg-rds p-3 text-sm text-rd">{error}</p>}
      <input className="w-full border-b border-ln bg-transparent px-0 pt-1 pb-2 text-xl font-semibold outline-none focus:border-ac" autoFocus
        aria-label={tr('Название страницы')} value={title} maxLength={120} onChange={(e) => setTitle(e.target.value)} />
      <span className={SEG} role="group" aria-label={tr('Режим редактирования')}>
        <button type="button" className={SEGBTN} aria-pressed={!preview} onClick={() => setPreview(false)}>{tr('Редактор')}</button>
        <button type="button" className={SEGBTN} aria-pressed={preview} onClick={() => setPreview(true)}>{tr('Просмотр')}</button>
      </span>
      {preview ? <Body md={body} c={c} /> : (
        <textarea className="w-full resize-y rounded-[10px] border border-ln bg-sf2 px-3.5 py-3 font-mono text-[13px] leading-[1.6] text-tx focus:border-ac focus:outline-0"
          aria-label={tr('Текст страницы в markdown')} value={body} rows={Math.max(14, body.split('\n').length + 1)} onChange={(e) => setBody(e.target.value)}
          placeholder={tr('Markdown: [[Другая страница]], код задачи вроде TRK-07, **жирный**, - [ ] чекбокс')} />
      )}
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" className={btnPri} disabled={busy} title="Ctrl/⌘ + S" onClick={() => submit(true)}>{tr('Сохранить')}</button>
        <button type="button" className={btn} disabled={busy} onClick={() => (!changed || c.leave()) && onDone()}>{tr('Отмена')}</button>
      </div>
    </div>
  )
}

function PageArea({ id, c, dirty, startEdit }: { id: number; c: Ctx; dirty: RefObject<boolean>; startEdit: boolean }) {
  const qc = useQueryClient()
  const base = `/p/${c.project.slug}/wiki`
  const dq = useQuery({ queryKey: ['page', id], queryFn: () => api.page(id) })
  const [editing, setEditing] = useState(startEdit)
  const del = useMutation({
    mutationFn: () => api.deletePage(id),
    onSuccess: () => { qc.removeQueries({ queryKey: ['page', id] }); qc.invalidateQueries(); navigate(base) },
  })
  // Task codes resolve against the tasks the server found in the saved text plus this project's tasks (for the preview of a draft).
  const tasks = useMemo(() => [...(dq.data?.links.tasks ?? []), ...c.tasks], [dq.data, c.tasks])
  const cc = { ...c, tasks }

  if (dq.isPending) return <SkeletonRows n={5} />
  if (dq.isError) {
    return dq.error instanceof ApiError && dq.error.code === 'not_found'
      ? <EmptyState icon="doc" title={tr('Страница не найдена')} action={<Link to={base} className={btn}>{tr('Вики')}</Link>} />
      : <ErrorState message={dq.error.message} onRetry={() => dq.refetch()} />
  }
  const d = dq.data
  if (editing) return <PageEditor key={d.id} page={d} c={cc} dirty={dirty} onDone={() => setEditing(false)} />
  return (
    <div className="flex flex-col gap-4 p-5 max-[640px]:p-4">
      <div className="flex flex-wrap items-start gap-2.5">
        <h2 className="min-w-0 flex-[1_1_200px] text-xl font-semibold tracking-tight break-words">{d.title}</h2>
        <span className="flex gap-2">
          <button type="button" className={btn} onClick={() => setEditing(true)}>{tr('Изменить')}</button>
          <ConfirmButton label={tr('Удалить')} armed={tr('Точно удалить?')} icon={<Icon name="trash" size={15} />} disabled={del.isPending} onConfirm={() => del.mutate()} />
        </span>
      </div>
      <span className={mc}>{tr('Обновлено {when}', { when: fmtStamp(d.updated_at) })}</span>
      {del.isError && <p role="alert" className="rounded-lg bg-rds p-3 text-sm text-rd">{errText(del.error)}</p>}
      {d.body_md.trim() ? <Body md={d.body_md} c={cc} /> : <p className="text-sm text-t3">{tr('Страница пока пуста. Нажмите «Изменить», чтобы написать.')}</p>}
      {d.backlinks.length > 0 && (
        <Block title={tr('Ссылаются сюда')}>
          {d.backlinks.map((b) => <li key={b.id}><Link to={`${base}/${b.id}`} className="text-act">{b.title}</Link></li>)}
        </Block>
      )}
      {d.links.tasks.length > 0 && (
        <Block title={tr('Задачи')}>
          {d.links.tasks.map((t) => (
            <li key={t.code}><Link to={hrefWith('task', t.code)} className="text-act"><span className="font-mono text-[12.5px]">{t.code}</span> <span className="text-t2">{t.title}</span></Link></li>
          ))}
        </Block>
      )}
    </div>
  )
}

export function Wiki({ project, pageId }: { project: Project; pageId?: number }) {
  const qc = useQueryClient()
  const pq = useQuery({ queryKey: ['pages', project.id], queryFn: () => api.pages(project.id) })
  const tq = useQuery({ queryKey: ['tasks', project.id], queryFn: () => api.tasks(project.key) }) // same key as the board
  const pages = useMemo(() => pq.data ?? [], [pq.data])
  const tasks = useMemo(() => tq.data ?? [], [tq.data])
  const dirty = useRef(false) // set by the open editor
  const [fresh, setFresh] = useState<number>() // a page created a moment ago opens straight in the editor
  const [notice, setNotice] = useState('')
  useEffect(() => { if (fresh !== undefined && pageId !== fresh) setFresh(undefined) }, [pageId, fresh])
  const leave = () => !dirty.current || confirm(tr('Есть несохранённые изменения. Уйти без сохранения?'))
  const create = useMutation({
    mutationFn: (title: string) => api.createPage(project.id, { title }),
    onSuccess: (p) => { setNotice(''); setFresh(p.id); qc.invalidateQueries(); navigate(`/p/${project.slug}/wiki/${p.id}`) },
    onError: (e) => setNotice(errText(e)),
  })
  const newPage = () => {
    if (!leave()) return
    const taken = new Set(pages.map((p) => p.title.trim().toLowerCase()))
    let n = pages.length + 1
    while (taken.has(tr('Новая страница {n}', { n }).toLowerCase())) n++
    create.mutate(tr('Новая страница {n}', { n }))
  }
  const c: Ctx = { project, pages, tasks, leave, createByTitle: (t) => create.mutate(t) }
  const newBtn = <button type="button" className={btnPri} disabled={create.isPending || !pq.isSuccess} onClick={newPage}><Icon name="plus" size={16} sw={2} />{tr('Новая страница')}</button>

  return (
    <div className="flex flex-col items-start gap-6 px-8 pt-6 pb-8 max-[640px]:px-4 lg:flex-row">
      <aside className="flex w-full flex-col gap-3 lg:w-[260px] lg:shrink-0">
        <span className="flex items-center justify-between gap-2"><span className={cap}>{tr('Страницы')}</span>{newBtn}</span>
        {pq.isPending && <div className="flex flex-col gap-2.5"><Skeleton h={28} /><Skeleton h={28} w="80%" /><Skeleton h={28} w="65%" /></div>}
        {pq.isError && <ErrorState message={pq.error.message} onRetry={() => pq.refetch()} />}
        {pq.isSuccess && pages.length > 0 && (
          <nav className="max-h-64 overflow-y-auto lg:max-h-none" aria-label={tr('Страницы')}><Tree pages={pages} current={pageId} slug={project.slug} leave={leave} /></nav>
        )}
      </aside>
      <section className={BOX + ' w-full min-w-0 flex-1'}>
        {notice && <p role="alert" className="m-4 rounded-lg bg-rds p-3 text-sm text-rd">{notice}</p>}
        {pageId !== undefined ? <PageArea key={pageId} id={pageId} c={c} dirty={dirty} startEdit={fresh === pageId} />
          : pq.isSuccess && pages.length === 0 ? (
            <EmptyState icon="doc" title={tr('В вики пока нет страниц')} hint={tr('Создайте первую страницу: заметки, решения, ссылки на задачи.')} action={newBtn} />
          ) : pq.isSuccess && <EmptyState icon="doc" title={tr('Выберите страницу')} hint={tr('Откройте страницу из списка или создайте новую.')} />}
      </section>
    </div>
  )
}
