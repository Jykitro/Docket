// Fails when a Russian string in src/ has no entry in src/messages.ts, or an entry has the wrong placeholders / plural forms.
// Run: bun run i18n (make check runs it). A Cyrillic string literal anywhere in the code is treated as a translation key.
import { deepStrictEqual } from 'node:assert'
import { readFileSync, readdirSync } from 'node:fs'
import { relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import { MESSAGES } from '../src/messages.ts' // Node and Bun strip the types, so the keys are exactly what the app sees

const src = fileURLToPath(new URL('../src/', import.meta.url))
const IGNORED = new Set(['не найд']) // compared with a server message, never shown

// Decodes the body of a JS string or template literal.
const unescape = (s) =>
  s.replace(/\\(u\{[0-9a-fA-F]+\}|u[0-9a-fA-F]{4}|x[0-9a-fA-F]{2}|\r\n|[\s\S])/g, (_, e) => {
    if (e[0] === 'u' || e[0] === 'x') return String.fromCodePoint(parseInt(e.replace(/[ux{}]/g, ''), 16))
    return { n: '\n', t: '\t', r: '\r', b: '\b', f: '\f', v: '\v', 0: '\0', '\n': '', '\r\n': '', '\u2028': '', '\u2029': '' }[e] ?? e
  })

// Yields [value, line] for every string literal and template literal without ${} in a .ts/.tsx file. It skips comments and
// regex literals; JSX text is treated as code, so a stray quote there swallows at most the rest of its line.
function* literals(text) {
  let i = 0
  let line = 1
  let prev = '' // last significant code character, to tell a regex from a division or a JSX closing tag
  const braces = [] // for each open ${ … }: the depth of plain braces inside it
  const at = (n = 0) => text[i + n] ?? ''
  const step = () => {
    if (text[i++] === '\n') line++
  }
  const template = function* () {
    // i is just past a backtick or the } that closes a ${ … }
    const start = i
    const startLine = line
    while (i < text.length && at() !== '`' && !(at() === '$' && at(1) === '{')) {
      if (at() === '\\') step()
      step()
    }
    const whole = text[start - 1] === '`' && at() === '`'
    if (whole) yield [unescape(text.slice(start, i)), startLine]
    if (at() === '`') step()
    else if (at() === '$') {
      step()
      step()
      braces.push(0)
    }
  }
  while (i < text.length) {
    const c = at()
    if (c === '/' && at(1) === '/') while (i < text.length && at() !== '\n') step()
    else if (c === '/' && at(1) === '*') {
      step()
      step()
      while (i < text.length && !(at() === '*' && at(1) === '/')) step()
      step()
      step()
    } else if (c === "'" || c === '"') {
      const start = ++i
      while (i < text.length && at() !== c && at() !== '\n') i += at() === '\\' ? 2 : 1
      const body = text.slice(start, i)
      if (at() === c) {
        yield [unescape(body), line]
        i++
      }
      line += body.split('\n').length - 1 // escaped line continuations
      prev = c
    } else if (c === '`') {
      step()
      yield* template()
      prev = '`'
    } else if (c === '/' && (prev === '' || '(,=:[!&|?{;+-*%~^'.includes(prev))) {
      step() // regex literal: up to the closing / outside a character class, on the same line
      let cls = false
      while (i < text.length && at() !== '\n' && (cls || at() !== '/')) {
        if (at() === '\\') i++
        else if (at() === '[') cls = true
        else if (at() === ']') cls = false
        i++
      }
      step()
      prev = 'a'
    } else if (c === '{' && braces.length) {
      braces[braces.length - 1]++
      step()
      prev = c
    } else if (c === '}' && braces.length && braces[braces.length - 1] === 0) {
      braces.pop()
      step()
      yield* template()
      prev = '`'
    } else {
      if (c === '}' && braces.length) braces[braces.length - 1]--
      if (!/\s/.test(c)) prev = c
      step()
    }
  }
}

// Self-check of the tokenizer on the cases it has to get right.
deepStrictEqual(
  [...literals("tr('a\\'b') // 'c'\n/* 'd' */ x = /'\"/g; <I />{tr(\"e\\\"f\")}\n`g${tr(`h`)}i` + `j\\u0041`")],
  [["a'b", 1], ['e"f', 2], ['h', 3], ['jA', 3]],
)

const files = (dir) => readdirSync(dir, { withFileTypes: true, recursive: true }).filter((e) => e.isFile() && /\.tsx?$/.test(e.name)).map((e) => `${e.parentPath}/${e.name}`)
const used = new Map() // key -> first "file:line"
for (const f of files(src)) {
  const name = relative(src, f)
  if (name === 'i18n.ts' || name === 'messages.ts') continue
  let text = readFileSync(f, 'utf8')
  // Server messages map to [ru, uk]: values, not keys. Blank them out line for line so the line numbers stay right.
  if (name === 'api.ts') text = text.replace(/const SERVER[\s\S]*?\n}\n/, (m) => m.replace(/[^\n]/g, ' '))
  for (const [s, line] of literals(text)) {
    if (used.has(s) || IGNORED.has(s)) continue
    if (/[А-Яа-яЁё]/.test(s) || s in MESSAGES) used.set(s, `src/${name}:${line}`)
  }
}
// The MCP self-check sends Russian labels and details that the UI passes to tr(): count those entries as used too.
const served = [...literals(readFileSync(new URL('../../internal/httpapi/mcpadmin.go', import.meta.url), 'utf8'))].map(([s]) => s)

const ph = (s) => [...new Set(s.match(/\{\w+\}/g) ?? [])].sort().join()
const problems = []
for (const [k, where] of used) if (!(k in MESSAGES)) problems.push(`${where}: missing translation: ${JSON.stringify(k)}`)
for (const [k, [en, uk]] of Object.entries(MESSAGES)) {
  if (ph(k) !== ph(en) || ph(k) !== ph(uk)) problems.push(`src/messages.ts: placeholders differ: ${JSON.stringify(k)}`)
  if (k.includes('|') && (en.split('|').length !== 2 || uk.split('|').length !== 3)) problems.push(`src/messages.ts: plural forms (en 2, uk 3): ${JSON.stringify(k)}`)
}
const unused = Object.keys(MESSAGES).filter((k) => !used.has(k) && !served.some((s) => s.includes(k)))
if (process.argv.includes('--unused')) for (const k of unused) console.log(`unused: ${JSON.stringify(k)}`)
if (problems.length) {
  console.error(problems.join('\n'))
  process.exit(1)
}
console.log(`i18n ok: ${used.size} strings used, ${Object.keys(MESSAGES).length} entries, ${unused.length} unused (--unused lists them)`)
