// Markdown for descriptions and notes. Everything goes through DOMPurify; links open in a new tab and
// task-list checkboxes are live (data-idx = their order in the source), see toggleCheckbox().
import DOMPurify from 'dompurify'
import { Marked } from 'marked'

let counter = 0
const md = new Marked({
  gfm: true,
  renderer: {
    checkbox({ checked }) {
      return `<input type="checkbox" data-idx="${counter++}"${checked ? ' checked' : ''}>`
    },
  },
})

DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName === 'A') {
    node.setAttribute('target', '_blank')
    node.setAttribute('rel', 'noopener noreferrer')
  }
})

export function renderMarkdown(src: string): string {
  counter = 0
  return DOMPurify.sanitize(md.parse(src, { async: false }), { ADD_ATTR: ['target', 'data-idx'] })
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
