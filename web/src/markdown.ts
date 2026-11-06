// Markdown for descriptions and notes. Everything goes through DOMPurify; links open in a new tab and
// task-list checkboxes are live (data-idx = their order in the source), see toggleCheckbox().
import DOMPurify from 'dompurify'
import { Marked } from 'marked'
import type { PageRef, TaskRef } from './api'

let counter = 0
const md = new Marked({
  gfm: true,
  renderer: {
    checkbox({ checked }) {
      return `<input type="checkbox" data-idx="${counter++}"${checked ? ' checked' : ''}>`
    },
  },
})

// Wiki anchors (data-wiki, data-wiki-new, data-task) with an in-app href stay in the same tab; any other anchor opens a new one.
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  const inApp = node.hasAttribute('data-wiki') || node.hasAttribute('data-wiki-new') || node.hasAttribute('data-task')
  if (node.tagName === 'A' && !(inApp && /^(?:\/(?!\/)|\?|#)/.test(node.getAttribute('href') ?? ''))) {
    node.setAttribute('target', '_blank')
    node.setAttribute('rel', 'noopener noreferrer')
  }
})

export function renderMarkdown(src: string): string {
  counter = 0
  return DOMPurify.sanitize(md.parse(src, { async: false }), { ADD_ATTR: ['target', 'data-idx'] })
}

type WikiCtx = { slug: string; pages: PageRef[]; tasks: TaskRef[] }
let wiki: WikiCtx = { slug: '', pages: [], tasks: [] }
const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
const titleKey = (t: string) => t.trim().toLowerCase()

// Second instance for the wiki: [[Title]] and task codes become links, resolved against the context set by renderWiki().
// Inline extensions never run inside code spans or blocks. The lookbehind lives in start(): the tokenizer sees only the rest of the line.
const mdWiki = new Marked({
  gfm: true,
  extensions: [
    {
      name: 'wikilink',
      level: 'inline',
      start: (src) => src.indexOf('[['),
      tokenizer(src) {
        const m = /^\[\[([^[\]\n]+)\]\]/.exec(src)
        if (m) return { type: 'wikilink', raw: m[0], title: m[1].trim() }
      },
      renderer(t) {
        const title = String(t.title)
        const page = wiki.pages.find((p) => titleKey(p.title) === titleKey(title))
        return page
          ? `<a href="/p/${esc(wiki.slug)}/wiki/${page.id}" data-wiki="${page.id}">${esc(title)}</a>`
          : `<a href="#" data-wiki-new="${esc(title)}" class="border-b border-dashed">${esc(title)}</a>`
      },
    },
    {
      name: 'taskcode',
      level: 'inline',
      start: (src) => src.search(/(?<![A-Za-z0-9])[A-Z][A-Z0-9]{1,4}-\d+\b/),
      tokenizer(src) {
        const m = /^[A-Z][A-Z0-9]{1,4}-\d+\b/.exec(src)
        if (m && wiki.tasks.some((t) => t.code === m[0])) return { type: 'taskcode', raw: m[0], code: m[0] }
      },
      renderer: (t) => `<a href="?task=${esc(String(t.code))}" data-task="${esc(String(t.code))}">${esc(String(t.code))}</a>`,
    },
  ],
})

/** Wiki page body: like renderMarkdown, plus [[Title]] links and task codes that exist in ctx.tasks. */
export function renderWiki(src: string, ctx: WikiCtx): string {
  wiki = ctx
  return DOMPurify.sanitize(mdWiki.parse(src, { async: false }), { ADD_ATTR: ['target'] })
}

const ITEM = /^(\s*(?:[-*+]|\d+[.)])\s+\[)([ xX])(\].*)$/

/** Flips the idx-th `- [ ]` / `- [x]` of the source (fenced code blocks are skipped, like the renderer does). */
export function toggleCheckbox(src: string, idx: number): string {
  const lines = src.split('\n')
  let fence = ''
  let n = 0
  for (let i = 0; i < lines.length; i++) {
    const f = lines[i].match(/^\s*(```|~~~)/)
    if (f) {
      fence = fence === '' ? f[1] : fence === f[1] ? '' : fence
      continue
    }
    if (fence) continue
    const m = lines[i].match(ITEM)
    if (!m) continue
    if (n++ === idx) {
      lines[i] = m[1] + (m[2] === ' ' ? 'x' : ' ') + m[3]
      break
    }
  }
  return lines.join('\n')
}

// marked produces plain HTML without classes, so the styling sits on the container as arbitrary variants.
// The class names are written out in full (not generated): Tailwind finds them by scanning this source text.
const MD_COMMON = [
  '[&>:first-child]:mt-0 break-words',
  '[&_:is(h1,h2,h3,h4)]:text-tx [&_:is(h1,h2,h3,h4)]:mt-[18px] [&_:is(h1,h2,h3,h4)]:mb-1.5 [&_:is(h1,h2,h3,h4)]:font-semibold [&_:is(h1,h2,h3,h4)]:leading-[1.35]',
  '[&_h1]:text-[18px] [&_h2]:text-[16px] [&_h3]:text-[15px] [&_h4]:text-[15px]',
  '[&_:is(ul,ol)]:mb-2 [&_:is(ul,ol)]:pl-[22px] [&_:is(ul,ol)]:flex [&_:is(ul,ol)]:flex-col [&_:is(ul,ol)]:gap-1',
  '[&_ul]:list-disc [&_ol]:list-decimal',
  '[&_li:has(>input[type=checkbox])]:list-none [&_li:has(>input[type=checkbox])]:-ml-[22px]',
  '[&_li:has(>p>input[type=checkbox])]:list-none [&_li:has(>p>input[type=checkbox])]:-ml-[22px]',
  '[&_[type=checkbox]]:accent-dn [&_[type=checkbox]]:size-4 [&_[type=checkbox]]:mr-2 [&_[type=checkbox]]:align-[-3px] [&_[type=checkbox]]:cursor-pointer',
  '[&_code]:font-mono [&_code]:text-[12.5px] [&_code]:bg-sk [&_code]:rounded-[5px] [&_code]:px-[5px] [&_code]:py-px',
  '[&_pre]:mt-1.5 [&_pre]:mb-2.5 [&_pre]:px-3.5 [&_pre]:py-3 [&_pre]:bg-sf2 [&_pre]:border [&_pre]:border-ln [&_pre]:rounded-[9px] [&_pre]:overflow-x-auto [&_pre]:leading-[1.6]',
  '[&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_pre_code]:text-[12.5px]',
  '[&_a]:text-act',
  '[&_blockquote]:mb-2 [&_blockquote]:pl-3 [&_blockquote]:border-l-[3px] [&_blockquote]:border-ln2 [&_blockquote]:text-t3',
  '[&_img]:max-w-full',
  '[&_table]:border-collapse [&_table]:mb-2',
  '[&_:is(th,td)]:border [&_:is(th,td)]:border-ln [&_:is(th,td)]:px-2 [&_:is(th,td)]:py-1'
].join(' ')

/** Descriptions. */
export const MD_PROSE = 'text-t2 text-[14.5px] leading-[1.6] [&_p]:mb-2 ' + MD_COMMON
/** Notes: a bit smaller and tighter. */
export const MD_NOTE = 'text-t2 text-[14px] leading-[1.55] [&_p]:mb-1 ' + MD_COMMON
