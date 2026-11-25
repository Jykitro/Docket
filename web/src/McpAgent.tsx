import { useState } from 'react'
import { Icon } from './icons'
import { tr } from './i18n'

const PANEL = 'overflow-hidden rounded-xl border border-ln bg-sf'
const BTN = 'inline-flex h-[30px] items-center gap-2 whitespace-nowrap rounded-md border border-ln bg-sf px-2.5 text-[13px] font-medium text-tx no-underline hover:bg-sk'
const PRI = 'inline-flex h-9 items-center gap-2 whitespace-nowrap rounded-md border border-ink bg-ink px-3 text-sm font-medium text-inkt hover:opacity-90'
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
const select = 'max-w-full rounded-md border border-ln bg-sf px-2 py-1.5 text-sm text-tx'

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
  const copyLabel = active === 'mcp' ? tr('Скопировать конфиг') : tr('Скопировать команду')
  return (
    <section className={PANEL} aria-labelledby="agent-install-title">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-ln px-5 py-4">
        <h2 id="agent-install-title" className="grow text-base font-semibold">{tr('Установка для агента')}</h2>
        <label className="flex items-center gap-2 text-sm text-t2">{tr('Клиент')}
          <select className={select} value={client} onChange={e => setClient(e.target.value as Client)}>
            {CLIENTS.map(name => <option key={name} value={name}>{tr(name)}</option>)}
          </select>
        </label>
      </div>
      <div className="grid min-w-0 gap-3 px-5 py-4">
        {cli && <div role="group" aria-label={tr('Способ установки')} className="flex flex-wrap gap-1.5">
          {(Object.keys(METHODS) as Method[]).map(m => (
            <button key={m} type="button" aria-pressed={m === active} onClick={() => setMethod(m)}
              className={BTN + ' aria-pressed:border-act aria-pressed:bg-acs aria-pressed:text-act'}>{tr(METHODS[m])}</button>
          ))}
        </div>}
        {useBoard && <div className="flex flex-wrap items-center gap-2 text-[13px] text-t2">
          <label className="flex items-center gap-2">{tr('Режим MCP')}
            <select className={select} value={codeMode ? 'code' : 'tools'} onChange={e => setCodeMode(e.target.value === 'code')}>
              <option value="tools">MCP</option><option value="code">Code Mode</option>
            </select>
          </label>
          <code className="min-w-0 break-all text-xs">{address}</code>
          <button type="button" className={BTN} aria-label={tr('Копировать адрес')} title={tr('Копировать адрес')} onClick={() => copy('url:' + address, () => address)}>
            <Icon name={copied === 'url:' + address ? 'check' : 'link'} size={14} />
          </button>
        </div>}
        {client === 'Другой клиент' && <label className="grid gap-1 text-sm text-t2">{tr('Имя агента')}
          <input className={select} value={agentName} maxLength={48} placeholder={tr('Из запроса MCP')} onChange={e => setAgentName(e.target.value)} />
        </label>}
        <p className="break-words text-sm text-t2">{help}</p>
        <div className="min-w-0 overflow-hidden rounded-lg border border-ln bg-sf2">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-ln px-3 py-2">
            <span className="text-xs font-medium text-t2">{cli ? tr('Терминал') : client === 'Claude Desktop' ? 'claude_desktop_config.json' : tr('Адрес сервера')}</span>
            <button type="button" className={PRI} onClick={() => copy(command, () => command)}>
              <Icon name={copied === command ? 'check' : 'doc'} size={14} /><span aria-live="polite">{copied === command ? tr('Скопировано') : copyLabel}</span>
            </button>
          </div>
          <pre className="whitespace-pre-wrap [overflow-wrap:anywhere] px-3 py-3 font-mono text-xs leading-relaxed" dir="ltr">{command}</pre>
        </div>
        {active !== 'skills' && <p className="text-xs text-t3">{tr('Для проверки попросите агента показать проекты выбранного пространства.')}</p>}
        {error && <p role="alert" className="text-sm text-rd">{error}</p>}
      </div>
      <details className="border-t border-ln px-5 py-3 text-[13px]">
        <summary className="cursor-pointer font-medium">{tr('Скачать отдельные навыки')}</summary>
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
        <details className="border-t border-ln px-5 py-3 text-[13px]">
          <summary className="cursor-pointer font-medium">{tr('Строгий режим (необязательно)')}</summary>
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
