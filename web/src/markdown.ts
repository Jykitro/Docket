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
