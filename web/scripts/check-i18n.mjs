// Fails when a Russian string in src/ has no entry in src/messages.ts, or an entry has the wrong placeholders / plural forms.
// Run: bun run i18n. A Cyrillic string literal anywhere in the code is treated as a translation key.
import { readFileSync, readdirSync } from 'node:fs'

const src = new URL('../src/', import.meta.url)
const messages = readFileSync(new URL('messages.ts', src), 'utf8')
const entries = new Map()
// An entry starts at the beginning of a line: 'key': ['en', 'uk'] (the pair may continue on the next lines).
for (const m of messages.matchAll(/^ {2}'((?:[^'\\]|\\.)*)':\s*\[\s*'((?:[^'\\]|\\.)*)',\s*'((?:[^'\\]|\\.)*)'/gm)) entries.set(m[1], [m[2], m[3]])

const IGNORED = new Set(['не найд']) // compared with a server message, never shown
const used = new Set()
for (const f of readdirSync(src).filter((f) => /\.tsx?$/.test(f) && f !== 'i18n.ts' && f !== 'messages.ts')) {
  let text = readFileSync(new URL(f, src), 'utf8')
  if (f === 'api.ts') text = text.replace(/const SERVER[\s\S]*?\n}\n/, '') // server messages map to [ru, uk], they are values, not keys
  for (const line of text.split('\n')) {
    if (/^\s*(\/\/|\/\*|\*)/.test(line)) continue
    for (const m of line.matchAll(/'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"/g)) {
      const s = m[0].slice(1, -1)
      if (/[А-Яа-яЁё]/.test(s) && !IGNORED.has(s)) used.add(s)
    }
  }
}

const ph = (s) => [...new Set(s.match(/\{\w+\}/g) ?? [])].sort().join()
const problems = []
for (const k of used) if (!entries.has(k)) problems.push(`missing translation: ${k}`)
for (const [k, [en, uk]] of entries) {
  if (ph(k) !== ph(en) || ph(k) !== ph(uk)) problems.push(`placeholders differ: ${k}`)
  if (k.includes('|') && (en.split('|').length !== 2 || uk.split('|').length !== 3)) problems.push(`plural forms (en 2, uk 3): ${k}`)
}
if (problems.length) {
  console.error(problems.join('\n'))
  process.exit(1)
}
console.log(`i18n ok: ${used.size} strings used, ${entries.size} entries`)
