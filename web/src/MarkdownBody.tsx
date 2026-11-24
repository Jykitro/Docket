import { type RefObject, useMemo } from 'react'
import { flushSync } from 'react-dom'
import { HtmlPreview } from './HtmlPreview'
import { tr } from './i18n'
import { MD_PROSE, renderBody } from './markdown'

export function MarkdownBody({ source, wiki }: { source: string; wiki?: Parameters<typeof renderBody>[1] }) {
  const parts = useMemo(() => renderBody(source, wiki), [source, wiki])
  return parts.map((part, i) => part.kind === 'preview'
    ? <HtmlPreview key={i} title={tr('HTML-вставка')} html={part.html} />
    : <div key={i} className={MD_PROSE} dangerouslySetInnerHTML={{ __html: part.html }} />)
}

export function InsertHTML({ editor, change }: { editor: RefObject<HTMLTextAreaElement | null>; change: (text: string) => void }) {
  function insert() {
    const el = editor.current
    if (!el) return
    const start = el.selectionStart
    const end = el.selectionEnd
    const snippet = el.value.slice(start, end) || '<p>HTML</p>'
    const longest = Array.from(snippet.matchAll(/`+/g), m => m[0].length)
    const fence = '`'.repeat(longest.reduce((length, size) => Math.max(length, size + 1), 3))
    const prefix = (start ? '\n\n' : '') + fence + 'html-preview\n'
    const block = prefix + snippet + '\n' + fence + (end < el.value.length ? '\n\n' : '\n')
    const value = el.value.slice(0, start) + block + el.value.slice(end)
    flushSync(() => change(value))
    el.focus()
    el.setSelectionRange(start + prefix.length, start + prefix.length + snippet.length)
  }
  return <button type="button" className="w-fit text-sm font-medium text-act hover:underline" onMouseDown={e => e.preventDefault()} onClick={insert}>{tr('Вставить HTML')}</button>
}
