import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, fmtStamp, type HTMLDocumentInfo, useWho } from './api'
import { tr } from './i18n'
import { ConfirmButton, ErrorState, SkeletonRows, btn, btnPri } from './ui'

type Owner = { task: string } | { page_id: number }
const input = 'rounded-md border border-ln bg-sf px-3 py-2 text-sm text-tx'
const limit = 2 * 1024 * 1024

// The trusted outer frame sets policy before any document markup is parsed. Its frame-src
// also restricts navigation of the inner frame. Neither frame inherits the app's origin.
function previewHTML(html: string) {
  const attr = html.replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
  const policy = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; media-src data: blob:; font-src data:; frame-src about:; form-action 'none'; base-uri 'none'; object-src 'none'; worker-src 'none'; connect-src 'none'"
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="${policy}"><meta name="referrer" content="no-referrer"><style>html,body,iframe{width:100%;height:100%;margin:0;border:0;display:block;background:white}</style></head><body><iframe title="HTML" sandbox="allow-scripts" referrerpolicy="no-referrer" srcdoc="${attr}"></iframe></body></html>`
}

function DocumentWindow({ doc, owner, close }: { doc?: HTMLDocumentInfo; owner: Owner; close: () => void }) {
  const ref = useRef<HTMLDialogElement>(null)
  const qc = useQueryClient()
  const who = useWho()
  const [version, setVersion] = useState(doc?.version ?? 0)
  const [editing, setEditing] = useState(!doc)
  const [title, setTitle] = useState(doc?.title ?? '')
  const [html, setHtml] = useState('')
  const [fileError, setFileError] = useState('')
  const fileNumber = useRef(0)
  const read = useQuery({
    queryKey: ['html-document', doc?.id, version],
    queryFn: () => api.readHTML(doc!.id, version),
    enabled: !!doc,
  })
  const preview = useMemo(() => read.data ? previewHTML(read.data.html) : '', [read.data])
  const save = useMutation({
    mutationFn: () => api.publishHTML({ ...owner, title, html, expected_version: version }),
    onSuccess: async () => { await qc.invalidateQueries(); close() },
  })
  const del = useMutation({
    mutationFn: () => api.deleteHTML(doc!.id, version),
    onSuccess: async () => { await qc.invalidateQueries(); close() },
  })
  useEffect(() => {
    const dialog = ref.current!
    dialog.showModal()
    return () => dialog.close()
  }, [])
  const busy = save.isPending || del.isPending
  async function loadFile(file?: File) {
    const current = ++fileNumber.current
    if (!file) return
    setFileError('')
    if (file.size > limit) { setFileError(tr('Файл превышает 2 МиБ')); return }
    try {
      const text = await file.text()
      if (current !== fileNumber.current) return
      setHtml(text)
      if (!title.trim()) setTitle(file.name.replace(/\.html?$/i, ''))
    } catch { setFileError(tr('Не удалось прочитать файл')) }
  }
  return <dialog role="dialog" aria-label={doc?.title ?? tr('Добавить HTML-документ')} ref={ref}
    className="fixed inset-0 m-0 h-dvh max-h-none w-screen max-w-none bg-bg p-0 text-tx backdrop:bg-black/50"
    onCancel={e => { e.preventDefault(); if (!busy) close() }}>
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center gap-2 border-b border-ln bg-sf px-4 py-3">
        <h2 className="min-w-0 flex-[1_1_180px] wrap-anywhere text-lg font-semibold">{doc?.title || tr('Добавить HTML-документ')}</h2>
        {doc && !editing && <>
          <label className="flex items-center gap-2 text-sm">{tr('Версия')}
            <select className={input} value={version} onChange={e => setVersion(Number(e.target.value))}>
              {Array.from({ length: doc.version }, (_, i) => doc.version - i).map(v => <option key={v}>{v}</option>)}
            </select>
          </label>
          {read.data && <span className="text-xs text-t3">{who(read.data.author)} · {fmtStamp(read.data.created_at)}</span>}
          <button className={btn} disabled={!read.data || version !== doc.version || busy} onClick={() => { setHtml(read.data!.html); setEditing(true) }}>{tr('Изменить')}</button>
          <ConfirmButton label={tr('Удалить')} armed={tr('Удалить все версии?')} disabled={version !== doc.version || busy} onConfirm={() => del.mutate()} />
        </>}
        <button className={btn} disabled={busy} onClick={close}>{tr('Закрыть')}</button>
      </header>
      {editing ? <form className="mx-auto flex min-h-0 w-full max-w-6xl grow flex-col gap-3 p-4" onSubmit={e => { e.preventDefault(); save.mutate() }}>
        <label className="grid gap-1 text-sm">{tr('Название')}<input required maxLength={120} readOnly={!!doc} className={input} value={title} onChange={e => setTitle(e.target.value)} /></label>
        <label className="grid gap-1 text-sm">{tr('Загрузить HTML-файл')}<input type="file" accept=".htm,.html,text/html" onChange={e => void loadFile(e.target.files?.[0])} /></label>
        {fileError && <p role="alert" className="text-sm text-rd">{fileError}</p>}
        <label className="flex min-h-0 grow flex-col gap-1 text-sm">{tr('Исходный HTML')}
          <textarea required spellCheck={false} className={input + ' min-h-28 grow resize-none font-mono'} value={html} onChange={e => setHtml(e.target.value)} />
        </label>
        <footer className="flex justify-end gap-2">
          <button type="submit" className={btnPri} disabled={!html.trim() || !title.trim() || busy}>{tr('Опубликовать')}</button>
        </footer>
      </form> : read.isPending ? <SkeletonRows /> : read.isError ? <ErrorState message={read.error.message} onRetry={() => read.refetch()} />
        : <iframe className="min-h-0 w-full grow border-0 bg-white" title={doc!.title} sandbox="allow-scripts" referrerPolicy="no-referrer" srcDoc={preview} />}
      {(save.isError || del.isError) && <p role="alert" className="p-3 text-sm text-rd">{(save.error || del.error)?.message}</p>}
    </div>
  </dialog>
}

export function HtmlDocuments({ docs = [], owner }: { docs?: HTMLDocumentInfo[]; owner: Owner }) {
  const [open, setOpen] = useState<number | 'new' | null>(null)
  const who = useWho()
  const doc = docs.find(d => d.id === open)
  return <section className="space-y-2">
    <header className="flex flex-wrap items-center justify-between gap-2">
      <h2 className="text-sm font-semibold">{tr('HTML-документы')}</h2>
      <button className={btn} onClick={() => setOpen('new')}>{tr('Добавить HTML-документ')}</button>
    </header>
    {docs.map(d => <button key={d.id} className="flex w-full flex-wrap items-center justify-between gap-2 rounded-md border border-ln bg-sf px-3 py-2 text-left hover:bg-sk" onClick={() => setOpen(d.id)}>
      <span className="min-w-0 wrap-anywhere text-sm font-medium">{d.title}</span>
      <span className="text-xs text-t3">{tr('Версия')} {d.version} · {who(d.author)} · {fmtStamp(d.created_at)}</span>
    </button>)}
    {(open === 'new' || doc) && <DocumentWindow key={open} doc={doc} owner={owner} close={() => setOpen(null)} />}
  </section>
}
