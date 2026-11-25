// MCP tab: the agent skill (how an assistant keeps Plandillo true) and the optional Stop hook for Claude Code.
import { useState } from 'react'
import { Icon } from './icons'
import { tr } from './i18n'

const PANEL = 'overflow-hidden rounded-xl border border-ln bg-sf'
const BTN = 'inline-flex h-[30px] items-center gap-2 whitespace-nowrap rounded-md border border-ln bg-sf px-2.5 text-[13px] font-medium text-tx no-underline hover:bg-sk'
const PRI = 'inline-flex h-9 items-center gap-2 whitespace-nowrap rounded-md border border-ink bg-ink px-3 text-sm font-medium text-inkt hover:opacity-90'
const PRE ='overflow-x-auto rounded-[10px] border border-ln bg-sf2 px-4 py-3 font-mono text-[12.5px] leading-[1.65] whitespace-pre text-t2'
const STEPS = [
  ['Старт', 'Находит или создаёт задачу, кладёт план чек-листом в описание и ставит «В работе».'],
  ['Ход работы', 'Отмечает шаги и пишет заметки. Найденные проблемы заводит задачами с областью issue.'],
  ['Финиш', 'Сверяет: шаги закрыты, итог в заметке, статус честный, ничего не зависло.'],
] as const
// Claude Code, Claude Desktop, Codex: where the skill file goes. Desktop has no skill folder, so it gets the text.
const SKILL_PATH = ['~/.claude/skills/<name>/SKILL.md', '', '~/.agents/skills/<name>/SKILL.md']
const SKILLS = [
  ['plandillo', 'Рабочий процесс: задача, план, заметки, финиш. Его же сервер отправляет при подключении.'],
  ['plandillo-reference', 'Справочник: инструменты, поля, ошибки и ответы на частые вопросы.'],
  ['plandillo-setup', 'Подключение клиентов, установка навыков, хуки и разбор проблем.'],
] as const
// One file works for both: Claude Code and Codex share the hooks format and the Stop contract.
// SessionStart stdout becomes context. Stop blocks the first stop of a turn and lets the second pass (stop_hook_active),
// so the reminder also lets read-only and untracked work finish.
const HOOK = `{
  "hooks": {
    "SessionStart": [{ "hooks": [{
      "type": "command",
      "command": "echo 'Plandillo: for tracked work, follow the Plandillo workflow. Answer board questions with reads and respect a request to skip tracking.'"
    }] }],
    "Stop": [{ "hooks": [{
      "type": "command",
      "command": "grep -Eq '\\"stop_hook_active\\": ?true' && exit 0; echo '{\\"decision\\":\\"block\\",\\"reason\\":\\"Plandillo end checklist: tick finished steps, add_note the result, move_tasks to done only if checks passed, record findings with report_issue and list their IDs, check get_board doing. For read-only requests or untracked work, finish without planner writes.\\"}'"
    }] }]
  }
}`
// Claude Code: the project file. Codex: its global file (hooks are on by default there).
const HOOK_PATH = ['.claude/settings.json', '', '~/.codex/hooks.json']

export const shellQuote = (value: string) => "'" + value.replaceAll("'", "'\"'\"'") + "'"

export function McpAgent({ tab, url }: { tab: number; url: string }) {
  const [copied, setCopied] = useState('')
  const [error, setError] = useState('')
  const copy = async (what: string, text: () => string | Promise<string>) => {
    try {
      await navigator.clipboard.writeText(await text())
      setError('')
      setCopied(what)
      setTimeout(() => setCopied(''), 1500)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }
  const skill = (name: string) => async () => {
    const r = await fetch('/api/mcp/skill/' + name)
    if (!r.ok) throw new Error(`${r.status} ${r.statusText}`)
    return r.text()
  }
  // The server may sit in Docker and cannot reach the user's home, so the page hands over one command that does everything.
  const install = () => `curl -fsSL ${shellQuote(location.origin + '/api/mcp/install.sh')} | PLANDILLO_URL=${shellQuote(url)} sh`
  const path = SKILL_PATH[tab]

  return (
    <section className={PANEL}>
      <div className="flex items-center gap-3 px-5 pt-4 pb-1">
        <h2 className="grow text-[16px] font-semibold tracking-[-.01em]">{tr('Навык для агента')}</h2>
      </div>
      <div className="flex flex-col gap-4 px-5 pb-5">
        <p className="text-[13px] text-t3">{tr('Plandillo сам отправляет агенту эти правила при подключении. Навык нужен, чтобы они срабатывали с первого сообщения.')}</p>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-[10px] border border-ln bg-sf2 px-4 py-3">
          <button type="button" className={PRI} onClick={() => copy('install', install)}>
            <Icon name={copied === 'install' ? 'check' : 'spark'} size={15} />{copied === 'install' ? tr('Команда скопирована') : tr('Установить одной командой')}
          </button>
          <span className="min-w-0 flex-[1_1_260px] text-[12.5px] text-t3">{tr('Установите навыки в терминале и подтвердите подключение в браузере. Для Claude Code откройте /mcp, для Codex выполните команду входа из установщика.')}</span>
        </div>
        <ol className="flex flex-col gap-1.5 text-[13px]">
          {STEPS.map(([name, text], i) => (
            <li key={name} className="flex gap-2.5">
              <span className="grid size-5 shrink-0 place-items-center rounded-full bg-sk font-mono text-[11px] font-semibold text-t2">{i + 1}</span>
              <span><b className="font-semibold">{tr(name)}.</b> <span className="text-t2">{tr(text)}</span></span>
            </li>
          ))}
        </ol>
        <div>
          <div className="mb-2 truncate font-mono text-[12.5px] text-t3">{path || tr('Вставьте в инструкции проекта')}</div>
          {SKILLS.map(([name, text]) => (
            <div key={name} className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-ln py-2.5">
              <span className="flex-[0_0_150px] font-mono text-[12.5px] font-medium">{name}</span>
              <span className="min-w-0 flex-[1_1_220px] text-[13px] text-t2">{tr(text)}</span>
              <button type="button" className={BTN} aria-label={tr('Скопировать SKILL.md') + ': ' + name} title={tr('Скопировать SKILL.md')} onClick={() => copy(name, skill(name))}>
                <Icon name={copied === name ? 'check' : 'doc'} size={14} />
              </button>
              <a className={BTN} aria-label={tr('Скачать') + ': ' + name} title={tr('Скачать')} href={'/api/mcp/skill/' + name} download={name + '-SKILL.md'}><Icon name="link" size={14} /></a>
            </div>
          ))}
        </div>
        {error && <div className="text-[13px] text-rd" role="alert">{error}</div>}
        {(tab === 0 || tab === 2) && (
          <div>
            <div className="mb-2 flex flex-wrap items-center gap-3">
              <span className="text-[12.5px] font-medium text-t2">{tr('Строгий режим (необязательно)')}</span>
              <span className="min-w-0 truncate font-mono text-[12px] text-t3">{HOOK_PATH[tab]}</span>
              <span className="grow" />
              <button type="button" className={BTN} onClick={() => copy('hook', () => HOOK)}>
                <Icon name={copied === 'hook' ? 'check' : 'doc'} size={14} />{copied === 'hook' ? tr('Скопировано') : tr('Скопировать хук')}
              </button>
            </div>
            <p className="mb-2 text-[12px] text-t3">{tr('Хуки напоминают агенту про задачу при старте и не дают закончить ход без сверки.')}</p>
            <pre className={PRE}>{HOOK}</pre>
          </div>
        )}
      </div>
    </section>
  )
}
