// Project wiki: the page tree on the left (stacked above on narrow screens), a page or its editor on the right.
// Pages link to each other with [[Title]] and to tasks by code; backlinks and mentioned tasks sit under the text.
import { type KeyboardEvent, type MouseEvent, type ReactNode, type RefObject, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, type Page, type PageDetail, type Project, type TaskRef, api, errText, fmtStamp } from './api'
import { HtmlDocuments, HtmlFileInput } from './HtmlDocuments'
import { Icon } from './icons'
import { tr } from './i18n'
import { toggleMarkdownCheckbox } from './markdown'
import { HtmlPreview } from './HtmlPreview'
import { InsertHTML, MarkdownBody } from './MarkdownBody'
import { Link, hrefWith, navigate, setParam } from './router'
import { WikiTree } from './WikiTree'
import { ConfirmButton, EmptyState, ErrorState, Skeleton, SkeletonRows, btn, btnPri, cap, mc } from './ui'

const BOX = 'bg-sf border border-ln rounded-xl'
const SEG = 'inline-flex self-start gap-0.5 rounded-md border border-ln p-0.5 text-[12.5px]'
const SEGBTN = 'rounded-md px-2.5 py-[3px] text-t3 aria-pressed:bg-sk aria-pressed:font-semibold aria-pressed:text-tx'

type Ctx = { project: Project; pages: Page[]; tasks: TaskRef[]; leave: () => boolean; createByTitle: (title: string) => void }

/** Rendered wiki markdown; clicks on its [[links]] and task chips are handled here by delegation. */
function Body({ md, c, onCheck, busy }: { md: string; c: Ctx; onCheck: (md: string) => void; busy?: boolean }) {
  const wiki = useMemo(() => ({ slug: c.project.slug, pages: c.pages, tasks: c.tasks }), [c.project.slug, c.pages, c.tasks])
  const click = (e: MouseEvent) => {
    const el = e.target
    if (el instanceof HTMLInputElement && el.type === 'checkbox' && el.dataset.idx !== undefined) {
      e.preventDefault()
      if (!busy) {
        const next = toggleMarkdownCheckbox(md, Number(el.dataset.idx), el.defaultChecked)
        if (next !== undefined) onCheck(next)
      }
      return
    }
    const a = (e.target as HTMLElement).closest('a')
    if (!a || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey) return
    const { wiki, wikiNew, task } = a.dataset
    if (wiki) { e.preventDefault(); if (c.leave()) navigate(`/p/${c.project.slug}/wiki/${wiki}`) }
    else if (task) { e.preventDefault(); setParam('task', task, false) }
    else if (wikiNew !== undefined) { e.preventDefault(); if (c.leave()) c.createByTitle(wikiNew) }
  }
  return <fieldset disabled={busy} aria-busy={busy} className="min-w-0 space-y-2" onClick={click}>
    <MarkdownBody source={md} wiki={wiki} />
  </fieldset>
}

function Block({ title, children }: { title: string; children: ReactNode }) {
  return <section className="border-t border-ln pt-4 flex flex-col gap-2"><h3 className="text-[13px] font-semibold text-t2">{title}</h3><ul className="flex flex-col gap-1 text-sm">{children}</ul></section>
}

function PageEditor({ page, initialTitle = '', c, dirty, onDone }: { page?: Page; initialTitle?: string; c: Ctx; dirty: RefObject<boolean>; onDone: (saved?: Page) => void }) {
  const qc = useQueryClient()
  const [orig, setOrig] = useState(page) // the version the draft started from; its updated_at is sent as expected_updated_at
  const [title, setTitle] = useState(page?.title ?? initialTitle)
  const [body, setBody] = useState(page?.body_html ?? page?.body_md ?? '')
  const [preview, setPreview] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [error, setError] = useState('')
  const [format, setFormat] = useState<'markdown' | 'html'>(page?.body_html !== undefined ? 'html' : 'markdown')
  const editor = useRef<HTMLTextAreaElement>(null)
  const originalBody = orig?.body_html ?? orig?.body_md ?? ''
  const changed = title.trim() !== (orig?.title ?? initialTitle) || body !== originalBody

  useEffect(() => {
    dirty.current = changed
    if (!changed) return
    const warn = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [changed, dirty])
  useEffect(() => () => { dirty.current = false }, [dirty])

  const load = (p: Page) => { setOrig(p); setTitle(p.title); setBody(p.body_html ?? p.body_md); setConflict(false); setError('') }
  const save = useMutation({
    mutationFn: async (v: { close: boolean; over?: boolean }) => {
      const source = format === 'html' ? { body_html: body } : { body_md: body }
      if (!orig) {
        const p = await api.createPage(c.project.id, { title, ...source })
        return { p, sent: { title: true, body: true } }
      }
      const expected = v.over ? (await api.page(orig.id)).updated_at : orig.updated_at
      const sent = { title: title.trim() !== orig.title, body: body !== originalBody }
      const p = await api.savePage(orig.id, {
        ...(sent.title && { title }), ...(sent.body && source), expected_updated_at: expected,
      })
      return { p, sent }
    },
    onSuccess: ({ p, sent }, v) => {
      // The baseline moves to the saved page; characters typed while the request was in flight stay in the draft (and keep it dirty).
      // A field that was not sent may have been changed by someone else (save over): take their version unless typed in meanwhile,
      // otherwise the next Save would send the stale draft and silently overwrite it.
      setTitle((t) => (sent.title || t.trim() !== orig?.title ? t : p.title))
      setBody((b) => (sent.body || b !== originalBody ? b : (p.body_html ?? p.body_md)))
      setOrig(p); setConflict(false); setError('')
      qc.setQueryData<PageDetail>(['page', p.id], (old) => old && { ...old, ...p })
      qc.invalidateQueries()
      if (!page || v.close) onDone(p)
    },
    onError: (e) => (e instanceof ApiError && e.code === 'conflict' ? setConflict(true) : setError(errText(e))),
  })
  const reload = useMutation({ mutationFn: () => api.page(page!.id), onSuccess: load, onError: (e) => setError(errText(e)) })
  const busy = save.isPending || reload.isPending
  const submit = (close: boolean) => {
    if (!title.trim() || busy) return
    if (!orig || changed) save.mutate({ close })
    else if (close) onDone()
  }
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
      {!page && <fieldset className="flex flex-wrap items-center gap-4 text-sm" disabled={busy}>
        <legend className="mb-2 font-semibold">{tr('Формат страницы')}</legend>
        {(['markdown', 'html'] as const).map(value => <label key={value} className="flex items-center gap-1.5">
          <input type="radio" name="wiki-format" checked={format === value} onChange={() => setFormat(value)} />{value === 'html' ? 'HTML' : 'Markdown'}
        </label>)}
      </fieldset>}
      <input disabled={busy} placeholder={tr('Название страницы')} className="w-full border-b border-ln bg-transparent px-0 pt-1 pb-2 text-xl font-semibold outline-none focus:border-ac" autoFocus
        aria-label={tr('Название страницы')} value={title} maxLength={120} onChange={(e) => setTitle(e.target.value)} />
      <span className={SEG} role="group" aria-label={tr('Режим редактирования')}>
        <button type="button" className={SEGBTN} aria-pressed={!preview} onClick={() => setPreview(false)}>{tr('Редактор')}</button>
        <button type="button" className={SEGBTN} aria-pressed={preview} onClick={() => setPreview(true)}>{tr('Просмотр')}</button>
      </span>
      {preview ? (format === 'html' ? <HtmlPreview title={title || 'HTML'} html={body} /> : <Body md={body} c={c} onCheck={setBody} busy={busy} />) : (<>
        {format === 'markdown' ? <InsertHTML editor={editor} change={setBody} /> :
          <HtmlFileInput onRead={(html, name) => { setBody(html); setTitle(current => current.trim() || name) }} />}
        <textarea className="w-full resize-y rounded-[10px] border border-ln bg-sf2 px-3.5 py-3 font-mono text-[13px] leading-[1.6] text-tx focus:border-ac focus:outline-0"
          ref={editor} disabled={busy} aria-label={format === 'html' ? tr('Исходный HTML') : tr('Текст страницы в markdown')} value={body} rows={Math.min(40, Math.max(14, body.split('\n').length + 1))} onChange={(e) => setBody(e.target.value)}
          placeholder={format === 'html' ? '<!doctype html>' : tr('Markdown: [[Другая страница]], код задачи вроде TRK-07, **жирный**, - [ ] чекбокс')} />
      </>)}
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" className={btnPri} disabled={busy || !title.trim()} title="Ctrl/⌘ + S" onClick={() => submit(true)}>{tr('Сохранить')}</button>
        <button type="button" className={btn} disabled={busy} onClick={() => (!changed || c.leave()) && onDone()}>{tr('Отмена')}</button>
      </div>
    </div>
  )
}

function PageArea({ id, c, dirty }: { id: number; c: Ctx; dirty: RefObject<boolean> }) {
  const qc = useQueryClient()
  const base = `/p/${c.project.slug}/wiki`
  const dq = useQuery({ queryKey: ['page', id], queryFn: () => api.page(id) })
  const [editing, setEditing] = useState(false)
  const check = useMutation({
    mutationFn: ({ body, revision }: { body: string; revision: string }) =>
      api.savePage(id, { body_md: body, expected_updated_at: revision }),
    onSuccess: (p) => {
      qc.setQueryData<PageDetail>(['page', id], (old) => old && { ...old, ...p })
      return qc.invalidateQueries({ queryKey: ['pages', c.project.id] })
    },
    onError: (e) => {
      if (e instanceof ApiError && e.code === 'conflict') return qc.invalidateQueries({ queryKey: ['page', id] })
    },
  })
  const del = useMutation({
    mutationFn: () => api.deletePage(id),
    onSuccess: () => { qc.removeQueries({ queryKey: ['page', id] }); qc.invalidateQueries(); navigate(base) },
  })
  // Task codes resolve against the tasks the server found in the saved text plus this project's tasks (for the preview of a draft).
  const tasks = useMemo(() => [...(dq.data?.links.tasks ?? []), ...c.tasks], [dq.data, c.tasks])
  const cc = { ...c, tasks }

  const notFound = <EmptyState icon="doc" title={tr('Страница не найдена')} action={<Link to={base} className={btn}>{tr('Вики')}</Link>} />
  if (dq.isPending) return <SkeletonRows n={5} />
  if (dq.isError) {
    return dq.error instanceof ApiError && dq.error.code === 'not_found' ? notFound : <ErrorState message={dq.error.message} onRetry={() => dq.refetch()} />
  }
  const d = dq.data
  if (d.project_id !== c.project.id) return notFound // an id from another project
  if (editing) return <PageEditor key={d.id} page={d} c={cc} dirty={dirty} onDone={() => setEditing(false)} />
  return (
    <div className="flex flex-col gap-4 p-5 max-[640px]:p-4">
      <div className="flex flex-wrap items-start gap-2.5">
        <h2 className="min-w-0 flex-[1_1_200px] text-xl font-semibold tracking-tight break-words">{d.title}</h2>
        <span className="flex gap-2">
          <button type="button" className={btn} disabled={check.isPending} onClick={() => { check.reset(); setEditing(true) }}>{tr('Изменить')}</button>
          <ConfirmButton label={tr('Удалить')} armed={tr('Точно удалить?')} icon={<Icon name="trash" size={15} />} disabled={del.isPending} onConfirm={() => del.mutate()} />
        </span>
      </div>
      <span className={mc}>{tr('Обновлено {when}', { when: fmtStamp(d.updated_at) })}</span>
      {del.isError && <p role="alert" className="rounded-lg bg-rds p-3 text-sm text-rd">{errText(del.error)}</p>}
      {check.isError && <p role="alert" className="rounded-lg bg-rds p-3 text-sm text-rd">
        {check.error instanceof ApiError && check.error.code === 'conflict'
          ? tr('Страница изменилась. Проверьте актуальный чеклист и повторите отметку.') : errText(check.error)}
      </p>}
      {d.body_html !== undefined ? <HtmlPreview title={d.title} html={d.body_html} /> : d.body_md.trim() ? <Body md={check.isPending ? check.variables.body : d.body_md} c={cc} busy={check.isPending || del.isPending}
        onCheck={(body) => check.mutate({ body, revision: d.updated_at })} /> : !d.documents?.length && <p className="text-sm text-t3">{tr('Страница пока пуста. Нажмите «Изменить», чтобы написать.')}</p>}
      {(d.body_html === undefined || !!d.documents?.length) && <HtmlDocuments key={d.id} docs={d.documents} owner={{ page_id: d.id }} />}
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
  const pq = useQuery({ queryKey: ['pages', project.id], queryFn: () => api.pages(project.id) })
  const tq = useQuery({ queryKey: ['tasks', project.id], queryFn: () => api.tasks(project.key) }) // same key as the board
  const pages = useMemo(() => pq.data ?? [], [pq.data])
  const tasks = useMemo(() => tq.data ?? [], [tq.data])
  const dirty = useRef(false) // set by the open editor
  const [draftTitle, setDraftTitle] = useState<string | null>(null)
  useEffect(() => { setDraftTitle(null) }, [project.id, pageId])
  const leave = () => {
    if (dirty.current && !confirm(tr('Есть несохранённые изменения. Уйти без сохранения?'))) return false
    setDraftTitle(null)
    return true
  }
  const newPage = (title = '') => {
    if (leave()) setDraftTitle(title)
  }
  const c: Ctx = { project, pages, tasks, leave, createByTitle: setDraftTitle }
  const newBtn = <button type="button" className={btnPri} disabled={draftTitle !== null || !pq.isSuccess} onClick={() => newPage()}><Icon name="plus" size={16} sw={2} />{tr('Новая страница')}</button>
  return (
    <div className="flex flex-col items-start gap-6 px-8 pt-6 pb-8 max-[640px]:px-4 lg:flex-row">
      <aside className="flex w-full flex-col gap-3 lg:w-[260px] lg:shrink-0">
        <span className="flex items-center justify-between gap-2"><span className={cap}>{tr('Страницы')}</span>{newBtn}</span>
        {pq.isPending && <div className="flex flex-col gap-2.5"><Skeleton h={28} /><Skeleton h={28} w="80%" /><Skeleton h={28} w="65%" /></div>}
        {pq.isError && <ErrorState message={pq.error.message} onRetry={() => pq.refetch()} />}
        {pq.isSuccess && pages.length > 0 && (
          <WikiTree key={project.id} pages={pages} current={pageId} project={project} leave={leave} />
        )}
      </aside>
      <section className={BOX + ' w-full min-w-0 flex-1'}>
        {draftTitle !== null ? <PageEditor key={'new:' + draftTitle} initialTitle={draftTitle} c={c} dirty={dirty} onDone={p => {
          setDraftTitle(null)
          if (p) navigate(`/p/${project.slug}/wiki/${p.id}`)
        }} /> : pageId !== undefined ? <PageArea key={pageId} id={pageId} c={c} dirty={dirty} />
          : pq.isSuccess && pages.length === 0 ? (
            <EmptyState icon="doc" title={tr('В вики пока нет страниц')} hint={tr('Создайте первую страницу: заметки, решения, ссылки на задачи.')} action={newBtn} />
          ) : pq.isSuccess && <EmptyState icon="doc" title={tr('Выберите страницу')} hint={tr('Откройте страницу из списка или создайте новую.')} />}
      </section>
    </div>
  )
}
