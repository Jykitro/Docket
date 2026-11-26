import { type ReactNode, useState } from 'react'
import { Icon } from './icons'
import { tr } from './i18n'
import { Select } from './ui'

const PANEL = 'rounded-xl border border-ln bg-sf' // no overflow-hidden: the selects' popovers must spill out
const BTN = 'inline-flex h-[30px] items-center gap-2 whitespace-nowrap rounded-md border border-ln bg-sf px-2.5 text-[13px] font-medium text-tx no-underline hover:bg-sk'
const COPY = 'absolute top-1.5 right-1.5 grid size-8 place-items-center rounded-md border border-ink bg-ink text-inkt hover:opacity-90'
const SUMMARY = 'flex cursor-pointer list-none items-center justify-between gap-2 font-medium [&::-webkit-details-marker]:hidden'
const CHEVRON = 'text-t3 motion-safe:transition-transform group-open:rotate-180'
const INPUT ='h-8 min-w-0 rounded-md border border-ln bg-sf px-2.5 text-[13px] text-tx'
const PRE = 'overflow-x-auto rounded-[10px] border border-ln bg-sf2 px-4 py-3 font-mono text-[12.5px] leading-[1.65] whitespace-pre text-t2'
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

const shellQuote = (value: string) => "'" + value.replaceAll("'", "'\"'\"'") + "'"

const CLIENTS = ['Claude Code', 'Codex', 'Claude Desktop', 'Другой клиент'] as const
type Client = typeof CLIENTS[number]
const METHODS = {
  workspace: 'Навыки и MCP этой доски',
  plugin: 'Плагин из GitHub',
  skills: 'Только навыки',
  mcp: 'Только MCP',
} as const
type Method = keyof typeof METHODS
const repo = 'huntly-digital/plandillo'
const local = 'http://localhost:7777/mcp' // the plugin's default board, skills/.mcp.json

function installCommand(client: Client, method: Method, address: string) {
  if (client === 'Другой клиент') return address
  if (client === 'Claude Desktop') return JSON.stringify({ mcpServers: { plandillo: {
    command: 'npx', args: ['-y', 'mcp-remote', address, ...(address.startsWith('http:') ? ['--allow-http'] : [])],
  } } }, null, 2)
  const codex = client === 'Codex'
  const agent = codex ? 'codex' : 'claude-code'
  const cli = codex ? 'codex' : 'claude'
  switch (method) {
    case 'workspace':
      return `curl -fsSL ${shellQuote(location.origin + '/api/mcp/install.sh')} | PLANDILLO_CLIENT=${agent} PLANDILLO_URL=${shellQuote(address)} sh`
    case 'plugin':
      return `${cli} plugin marketplace add ${repo}\n${cli} plugin ${codex ? 'add' : 'install'} plandillo@huntly-digital`
    case 'skills':
      return `npx skills add ${repo} --skill plandillo plandillo-reference plandillo-setup --agent ${agent}`
    case 'mcp':
      return codex ? `codex mcp add plandillo --url ${shellQuote(address)}`
        : `claude mcp add --transport http plandillo ${shellQuote(address)}\nclaude mcp login plandillo`
  }
}

/** A numbered step of the connect sequence; the numbers are real order, so they stay in an <ol>. */
function Step({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  return (
    <li className="grid grid-cols-[24px_minmax(0,1fr)] gap-3">
      <span aria-hidden="true" className="grid size-6 place-items-center rounded-full border border-ln2 text-xs font-semibold text-t2">{n}</span>
      <div className="grid min-w-0 gap-2"><span className="pt-0.5 text-sm font-medium">{title}</span>{children}</div>
    </li>
  )
}

export function McpAgent() {
  const [client, setClient] = useState<Client>('Claude Code')
  const [method, setMethod] = useState<Method>(location.origin + '/mcp' === local ? 'plugin' : 'workspace')
  const [codeMode, setCodeMode] = useState(false)
  const [agentName, setAgentName] = useState('')
  const cli = client === 'Claude Code' || client === 'Codex'
  const active = cli ? method : 'mcp'
  const useBoard = active === 'workspace' || active === 'mcp'
  const endpoint = location.origin + (codeMode ? '/mcp/code' : '/mcp')
  const address = client === 'Другой клиент' && agentName.trim() ? endpoint + '?' + new URLSearchParams({ agent: agentName.trim() }) : endpoint
  const command = installCommand(client, active, address)
  const path = client === 'Codex' ? '~/.agents/skills/<name>/SKILL.md' : '~/.claude/skills/<name>/SKILL.md'
  const hookPath = client === 'Codex' ? '~/.codex/hooks.json' : '.claude/settings.json'

  const [copied, setCopied] = useState('')
  const [error, setError] = useState('')
  const copy = async (what: string, text: () => string | Promise<string>) => {
    try {
      if (!navigator.clipboard) throw new Error(tr('Буфер обмена недоступен. Выделите и скопируйте текст вручную.'))
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
  const instructions = {
    workspace: 'Запустите в терминале на macOS или Linux. Установщик добавит три навыка и MCP в выбранный клиент. Подтвердите вход в браузере или воспользуйтесь командой входа из терминала.',
    plugin: client === 'Codex' ? 'Плагин содержит три навыка и MCP локальной доски {url}. Для другой доски выберите «Навыки и MCP этой доски». Выполните команды, откройте новый сеанс клиента и подтвердите доступ к пространству в настройках MCP.'
      : 'Плагин содержит три навыка и MCP локальной доски {url}. Для другой доски запускайте Claude Code с PLANDILLO_URL={board}. Выполните команды, откройте новый сеанс клиента и подтвердите доступ к пространству в настройках MCP.',
    skills: 'Запустите в папке проекта с установленным Node.js. Команда установит три навыка из GitHub. Для доступа к задачам добавьте MCP этой доски.',
    mcp: client === 'Codex' ? 'Выполните команду и подтвердите доступ в браузере. Если вход не открылся, выполните codex mcp login plandillo.'
      : 'Выполните команды в терминале и подтвердите доступ к пространству в браузере.',
  } as const
  const help = client === 'Claude Desktop' ? tr('Добавьте запись в mcpServers файла claude_desktop_config.json и перезапустите Claude Desktop. Для запуска npx нужен Node.js.')
    : client === 'Другой клиент' ? tr('Вставьте адрес в настройки клиента, выберите Streamable HTTP и подтвердите вход через браузер.')
    : tr(instructions[active], { url: local, board: endpoint })
  const copyLabel = client === 'Claude Desktop' ? tr('Скопировать конфиг') : client === 'Другой клиент' ? tr('Копировать адрес') : tr('Скопировать команду')
  const stepTitle = cli ? tr('Выполните в терминале') : client === 'Claude Desktop' ? tr('Добавьте конфиг в Claude Desktop') : tr('Вставьте адрес в клиент')
  return (
    <section className={PANEL} aria-labelledby="agent-install-title">
      <h2 id="agent-install-title" className="px-4 pt-3.5 pb-1 text-[15px] font-semibold">{tr('Подключить агента')}</h2>
      <ol className="flex flex-col gap-4 px-4 pt-2 pb-4">
        <Step n={1} title={tr('Выберите клиент и состав')}>
          <div className="flex flex-wrap gap-2">
            <Select label={tr('Клиент')} showLabel value={client} options={CLIENTS.map((c) => ({ value: c, label: tr(c) }))} onChange={setClient} />
            {cli && <Select label={tr('Способ установки')} value={active} options={(Object.keys(METHODS) as Method[]).map((m) => ({ value: m, label: tr(METHODS[m]) }))} onChange={setMethod} />}
            {useBoard && <Select label={tr('Режим MCP')} showLabel value={codeMode ? 'code' : 'tools'} options={[{ value: 'tools', label: 'MCP' }, { value: 'code', label: 'Code Mode' }]} onChange={(v) => setCodeMode(v === 'code')} />}
          </div>
          {client === 'Другой клиент' && <label className="flex flex-wrap items-center gap-2 text-[13px] text-t2">{tr('Имя агента')}
            <input className={INPUT} value={agentName} maxLength={48} placeholder={tr('Из запроса MCP')} onChange={e => setAgentName(e.target.value)} />
          </label>}
        </Step>
        <Step n={2} title={stepTitle}>
          <div className="relative min-w-0 rounded-lg border border-ln bg-sf2">
            <pre className="whitespace-pre-wrap [overflow-wrap:anywhere] py-2.5 pr-12 pl-3 font-mono text-xs leading-relaxed" dir="ltr">{command}</pre>
            <button type="button" className={COPY} aria-label={copyLabel} title={copyLabel} onClick={() => copy(command, () => command)}>
              <Icon name={copied === command ? 'check' : 'doc'} size={14} />
            </button>
            <span className="sr-only" aria-live="polite">{copied === command ? tr('Скопировано') : ''}</span>
          </div>
          <p className="break-words text-[13px] leading-snug text-t2">{help}</p>
          {error && <p role="alert" className="text-sm text-rd">{error}</p>}
        </Step>
        {active !== 'skills' && <Step n={3} title={tr('Проверьте подключение')}>
          <p className="text-[13px] text-t2">{tr('Попросите агента показать проекты этого пространства. Вызов появится в журнале вызовов.')}</p>
        </Step>}
      </ol>
      <details className="group border-t border-ln px-4 py-2.5 text-[13px]">
        <summary className={SUMMARY}>{tr('Скачать отдельные навыки')}<span className={CHEVRON}><Icon name="chevron" size={15} /></span></summary>
        <div className="pt-3">
          <div className="mb-2 break-all font-mono text-xs text-t3">{cli ? path : tr('Вставьте в инструкции проекта')}</div>
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
      </details>
      {cli && (
        <details className="group border-t border-ln px-4 py-2.5 text-[13px]">
          <summary className={SUMMARY}>{tr('Строгий режим (необязательно)')}<span className={CHEVRON}><Icon name="chevron" size={15} /></span></summary>
          <div className="mt-3 mb-2 flex flex-wrap items-center gap-3">
            <span className="min-w-0 truncate font-mono text-[12px] text-t3">{hookPath}</span>
            <span className="grow" />
            <button type="button" className={BTN} onClick={() => copy('hook', () => HOOK)}>
              <Icon name={copied === 'hook' ? 'check' : 'doc'} size={14} />{copied === 'hook' ? tr('Скопировано') : tr('Скопировать хук')}
            </button>
          </div>
          <p className="mb-2 text-[12px] text-t3">{tr('Хуки напоминают агенту про задачу при старте и не дают закончить ход без сверки.')}</p>
          <pre className={PRE}>{HOOK}</pre>
        </details>
      )}
    </section>
  )
}
