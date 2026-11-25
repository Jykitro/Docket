import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, fmtStamp, type HTMLDocumentInfo, useWho } from './api'
import { HtmlPreview } from './HtmlPreview'
import { tr } from './i18n'
import { ConfirmButton, ErrorState, SkeletonRows, btn, btnPri } from './ui'

type Owner = { task: string } | { page_id: number }
type Revision = HTMLDocumentInfo & { html: string }
const input = 'rounded-md border border-ln bg-sf px-3 py-2 text-sm text-tx'
const limit = 2 * 1024 * 1024

export function HtmlFileInput({ onRead }: { onRead: (html: string, name: string) => void }) {
  const [fileError, setFileError] = useState('')
  const fileNumber = useRef(0)
  async function loadFile(file?: File) {
    const current = ++fileNumber.current
    if (!file) return
    setFileError('')
    if (file.size > limit) { setFileError(tr('Файл превышает 2 МиБ')); return }
    try {
      const text = await file.text()
      if (current !== fileNumber.current) return
      onRead(text, file.name.replace(/\.html?$/i, ''))
    } catch { setFileError(tr('Не удалось прочитать файл')) }
  }
  return <>
    <label className="grid gap-1 text-sm">{tr('Загрузить HTML-файл')}<input type="file" accept=".htm,.html,text/html" onChange={e => void loadFile(e.target.files?.[0])} /></label>
    {fileError && <p role="alert" className="text-sm text-rd">{fileError}</p>}
  </>
}

function DocumentEditor({ initial, owner, close }: { initial?: Revision; owner: Owner; close: () => void }) {
  const qc = useQueryClient()
  const [title, setTitle] = useState(initial?.title ?? '')
  const [html, setHtml] = useState(initial?.html ?? '')
  const save = useMutation({
    mutationFn: () => api.publishHTML({ ...owner, title, html, expected_version: initial?.version ?? 0 }),
    onSuccess: async () => { await qc.invalidateQueries(); close() },
  })
  return <form className="grid gap-3" onSubmit={e => { e.preventDefault(); save.mutate() }}>
    <label className="grid gap-1 text-sm">{tr('Название')}<input required maxLength={120} readOnly={!!initial} className={input} value={title} onChange={e => setTitle(e.target.value)} /></label>
    <HtmlFileInput onRead={(text, name) => { setHtml(text); setTitle(current => current.trim() || name) }} />
    <label className="grid min-w-0 gap-1 text-sm">{tr('Исходный HTML')}
      <textarea required rows={12} spellCheck={false} className={input + ' w-full resize-y font-mono'} value={html} onChange={e => setHtml(e.target.value)} />
    </label>
    {save.isError && <p role="alert" className="text-sm text-rd">{save.error.message}</p>}
    <footer className="flex flex-wrap gap-2">
      <button type="submit" className={btnPri} disabled={!html.trim() || !title.trim() || save.isPending}>{tr('Опубликовать')}</button>
      <button type="button" className={btn} disabled={save.isPending} onClick={close}>{tr('Отмена')}</button>
    </footer>
  </form>
}

function InlineDocument({ doc, owner }: { doc: HTMLDocumentInfo; owner: Owner }) {
  const who = useWho()
  const qc = useQueryClient()
  const [previous, setPrevious] = useState<number | null>(null)
  const [draft, setDraft] = useState<Revision | null>(null)
  const version = previous ?? doc.version
  const read = useQuery({
    queryKey: ['html-document', doc.id, version],
    queryFn: () => api.readHTML(doc.id, version),
    gcTime: 0, staleTime: 'static', // Each published revision is immutable.
  })
  const del = useMutation({
    mutationFn: () => api.deleteHTML(doc.id, version),
    onSuccess: () => qc.invalidateQueries(),
  })
  if (draft) return <DocumentEditor owner={owner} initial={draft} close={() => setDraft(null)} />
  return <article aria-label={doc.title} className="min-w-0 space-y-2">
    {read.isPending ? <SkeletonRows /> : read.isError ? <ErrorState error={read.error} onRetry={() => read.refetch()} />
      : <HtmlPreview key={version} title={doc.title} html={read.data.html} />}
    <details className="text-xs text-t3">
      <summary className="w-fit max-w-full cursor-pointer wrap-anywhere hover:text-tx">{doc.title} · {tr('Версия')} {version}</summary>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <label className="flex items-center gap-2">{tr('Версия')}
          {doc.version <= 50 ? <select className={input} value={version} onChange={e => setPrevious(Number(e.target.value) === doc.version ? null : Number(e.target.value))}>
            {Array.from({ length: doc.version }, (_, i) => doc.version - i).map(v => <option key={v}>{v}</option>)}
          </select> : <input key={version} className={input + ' w-24'} type="number" min={1} max={doc.version} defaultValue={version}
            onBlur={e => {
              const value = e.currentTarget.valueAsNumber
              if (Number.isInteger(value) && value >= 1 && value <= doc.version) setPrevious(value === doc.version ? null : value)
              else e.currentTarget.value = String(version)
            }} onKeyDown={e => { if (e.key === 'Enter') e.currentTarget.blur() }} />}
        </label>
        {read.data && <span>{who(read.data.author)} · {fmtStamp(read.data.created_at)}</span>}
        <button className={btn} disabled={!read.data || version !== doc.version || del.isPending} onClick={() => setDraft(read.data!)}>{tr('Изменить')}</button>
        <ConfirmButton label={tr('Удалить')} armed={tr('Удалить все версии?')} disabled={version !== doc.version || del.isPending} onConfirm={() => del.mutate()} />
      </div>
    </details>
    {del.isError && <p role="alert" className="text-sm text-rd">{del.error.message}</p>}
  </article>
}

// Fetch a revision only when its preview comes into view. Keep an opened document mounted
// so scrolling down and back does not discard in-frame interactions or unsaved editor text.
function LazyDocument({ doc, owner }: { doc: HTMLDocumentInfo; owner: Owner }) {
  const host = useRef<HTMLDivElement>(null)
  const [opened, setOpened] = useState(false)
  useEffect(() => {
    if (opened || !host.current) return
    const observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) setOpened(true)
    })
    observer.observe(host.current)
    return () => observer.disconnect()
  }, [opened])
  return <div ref={host}>{opened ? <InlineDocument doc={doc} owner={owner} />
    : <button className="min-h-40 w-full text-left text-sm text-t3" onClick={() => setOpened(true)}>{doc.title}</button>}</div>
}

export function HtmlDocuments({ docs = [], owner }: { docs?: HTMLDocumentInfo[]; owner: Owner }) {
  const [adding, setAdding] = useState(false)
  return <div className="min-w-0 space-y-3">
    {docs.map(doc => <LazyDocument key={doc.id} doc={doc} owner={owner} />)}
    {adding ? <DocumentEditor owner={owner} close={() => setAdding(false)} />
      : <button type="button" className="text-xs font-medium text-t3 hover:text-tx hover:underline" onClick={() => setAdding(true)}>{tr('Добавить HTML-документ')}</button>}
  </div>
}
