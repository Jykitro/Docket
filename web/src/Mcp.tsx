// MCP tab: connection status and self-check, connection settings and token, tool switches, live call log.
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type McpTool, api, fmtTime } from './api'
import { Icon } from './icons'
import { locale, tr } from './i18n'
import { McpAgent } from './McpAgent'
import { ConfirmButton, ErrorState, Skeleton, Toggle } from './ui'

const BTN_LOOK = 'items-center gap-2 whitespace-nowrap rounded-md border font-medium no-underline disabled:cursor-default disabled:opacity-50'
const BTN_PRI = 'inline-flex h-9 px-3 text-sm border-ink bg-ink text-inkt hover:opacity-90 ' + BTN_LOOK
const BTN_SM = 'inline-flex h-[30px] px-2.5 text-[13px] border-ln bg-sf text-tx hover:bg-sk ' + BTN_LOOK
const BTN_SM_SQ = 'inline-flex size-[30px] justify-center p-0 text-[13px] border-ln bg-sf text-tx hover:bg-sk ' + BTN_LOOK
const PANEL = 'overflow-hidden rounded-xl border border-ln bg-sf'
const PH_ROW = 'flex items-center gap-3 px-5 pt-4'
const PH_H2 = 'grow text-[16px] font-semibold tracking-[-.01em]'
const LB = 'flex min-w-0 flex-col gap-1.5 text-[12.5px] font-medium text-t2'
const FLD = 'flex h-[38px] min-w-0 items-center gap-2 rounded-md border border-ln bg-sf2 pr-1.5 pl-3 text-[13px]'
const HINT = 'text-[12px] font-normal text-t3'
const ERR_PAD = 'px-5 pb-3.5 text-[13px] text-rd'
const CK = 'flex flex-wrap items-center gap-3 border-t border-ln px-5 py-[11px]'
const TL = 'flex items-center gap-3 border-t border-ln px-5 py-2.5'
const LG = 'items-center gap-2.5 border-t border-ln px-[18px] py-[9px] text-[12.5px]'
const SEG = 'inline-flex gap-0.5 rounded-md border border-ln p-0.5 text-[12.5px]'
const SEGBTN = 'rounded-md px-2.5 py-[3px] text-t3 disabled:cursor-default disabled:opacity-50 '
const FC = 'inline-flex h-7 items-center gap-1.5 whitespace-nowrap rounded-full border border-ln bg-sf px-2.5 text-[12.5px] text-t2 hover:bg-sk aria-pressed:border-transparent aria-pressed:bg-acs aria-pressed:font-medium aria-pressed:text-act aria-pressed:hover:bg-sk'
const MONO = 'font-mono text-[11.5px] text-t3'
const CODE_TAB = ['Claude Code', 'Claude Desktop', 'Codex'] as const

// The server writes its check texts in Russian. Fixed parts go through tr(); a trailing ": value" or timestamp stays as is.
const detail = (s: string) => s.split(' · ').map((p) => {
  const m = p.match(/^(.*?)(: .*| \d\d\.\d\d \d\d:\d\d)$/)
  return m ? tr(m[1]) + m[2] : tr(p)
}).join(' · ')
const unit = (v: string) => v.replace(/ (мс|МБ)$/, (_m, u: string) => ' ' + tr(u))

function copyText(text: string) {
  return navigator.clipboard?.writeText(text)
}

function config(tab: number, address: string, token: string) {
  if (tab === 0) return `claude mcp add --transport http docket ${address} \\\n  --header "Authorization: Bearer ${token}"`
  // Codex reads the token from an environment variable, so it never lands in config.toml.
  if (tab === 2) return `export DOCKET_MCP_TOKEN=${token}\ncodex mcp add docket --url ${address} \\\n  --bearer-token-env-var DOCKET_MCP_TOKEN`
  return `{\n  "mcpServers": {\n    "docket": {\n      "command": "npx",\n      "args": ["-y", "mcp-remote", "${address}",\n               "--header", "Authorization:\${AUTH}", "--allow-http"],\n      "env": { "AUTH": "Bearer ${token}" }\n    }\n  }\n}`
}

export function Mcp() {
  const qc = useQueryClient()
  const status = useQuery({ queryKey: ['mcp-status'], queryFn: api.mcpStatus })
  const settings = useQuery({ queryKey: ['mcp-settings'], queryFn: api.mcpSettings })
  const [filter, setFilter] = useState<'all' | 'write' | 'error'>('all')
  const calls = useQuery({ queryKey: ['mcp-calls', filter], queryFn: () => api.mcpCalls(filter) })
  const errors = useQuery({ queryKey: ['mcp-calls', 'error'], queryFn: () => api.mcpCalls('error') })

  const [checked, setChecked] = useState(false)
  const check = useMutation({
    mutationFn: api.mcpCheck,
    onSuccess: () => { setChecked(true); qc.invalidateQueries({ queryKey: ['mcp-status'] }) },
  })
  const patch = useMutation({
    mutationFn: api.patchMcpSettings,
    onSuccess: (d) => qc.setQueryData(['mcp-settings'], d),
    onSettled: () => qc.invalidateQueries({ queryKey: ['mcp-settings'] }),
  })

  const [tab, setTab] = useState(0)
  const [token, setToken] = useState<string | null>(null) // full token once revealed or rotated
  const [rotated, setRotated] = useState(false)
  const reveal = useMutation({ mutationFn: api.mcpToken, onSuccess: (t) => setToken(t.token) })
  const rotate = useMutation({
    mutationFn: api.rotateMcpToken,
    onSuccess: (t) => { setToken(t.token); setRotated(true); qc.invalidateQueries({ queryKey: ['mcp-settings'] }) },
  })
  const clear = useMutation({ mutationFn: api.clearMcpCalls, onSettled: () => qc.invalidateQueries({ queryKey: ['mcp-calls'] }) })
  const [copied, setCopied] = useState('')
  const flash = (what: string) => { setCopied(what); setTimeout(() => setCopied(''), 1500) }
  const fullToken = async () => token ?? (await api.mcpToken()).token

  const address = `${location.protocol}//${location.host}/mcp`
  const s = settings.data
  const st = status.data
  const result = st?.check
  const online = !!st?.online
  const enabled = s?.tools.filter((t) => t.enabled).length ?? 0
  const masked = s?.token_masked ?? 'dkt_••••'
  const shownToken = token ?? masked

  const groups: [string, McpTool[]][] = [
    ['Чтение', (s?.tools ?? []).filter((t) => t.group === 'read')],
    ['Запись', (s?.tools ?? []).filter((t) => t.group === 'write')],
  ]
  const readOnly = s?.access_mode === 'ro'

  return (
    <>
      <header className="flex flex-wrap items-center gap-3.5 px-8 pt-6 pb-4 max-[640px]:px-4">
        <span className="grid size-10 place-items-center rounded-[11px] bg-sk"><Icon name="plug" size={20} sw={1.9} /></span>
        <div className="flex min-w-0 flex-[1_1_260px] flex-col gap-0.5">
          <h1 className="text-2xl font-bold tracking-tight">MCP</h1>
          <span className="text-sm text-t3">{tr('Так Claude читает и ведёт задачи. Сервер работает внутри Docket, отдельно ничего ставить не нужно.')}</span>
        </div>
        {st && (
          <span className={'inline-flex h-[30px] items-center gap-2 rounded-full px-3 text-[13px] font-semibold ' + (online ? 'bg-dns text-dnt' : 'bg-rds text-rd')} role="status">
            <span className={'inline-block size-2 shrink-0 rounded-full ' + (online ? 'bg-dn' : 'bg-rd')} />
            {online ? tr('Онлайн · {v}', { v: unit(result?.checks.find((c) => c.key === 'server')?.value ?? '') }) : tr('Недоступен')}
          </span>
        )}
      </header>

      <div className="flex flex-wrap items-start gap-6 px-8 pt-6 pb-10 max-[640px]:px-4 max-[640px]:py-4">
        <div className="flex min-w-0 flex-[999_1_540px] flex-col gap-5">
          <section className={PANEL}>
            <div className="flex flex-wrap items-center gap-4 p-5">
              <span className={'grid size-[46px] shrink-0 place-items-center rounded-full ' + (online || !result ? 'bg-dns text-dnt' : 'bg-rds text-rd')}>
                <Icon name={online || !result ? 'check' : 'x'} size={22} sw={2.2} />
              </span>
              <div className="flex min-w-0 flex-[1_1_220px] flex-col gap-0.5">
                <span className="text-[19px] font-bold tracking-[-.01em]">{!result ? tr('Проверка…') : online ? tr('Подключено') : tr('Есть проблемы')}</span>
                <span className="text-[13px] text-t3">
                  {st?.last_call_at ? tr('Последний вызов в {time}. ', { time: fmtTime(st.last_call_at) }) : tr('Вызовов ещё не было. ')}
                  {result && tr('Проверка: {when}, {ok} из {n}.', { when: checked ? tr('только что') : fmtTime(result.at), ok: result.checks.filter((c) => c.ok).length, n: result.checks.length })}
                </span>
              </div>
              <button type="button" className={BTN_PRI} disabled={check.isPending} onClick={() => check.mutate()}>
                <Icon name="clock" size={15} />{check.isPending ? tr('Проверяю…') : checked ? tr('Проверено только что') : tr('Проверить соединение')}
              </button>
            </div>
            {status.isError && <ErrorState message={status.error.message} onRetry={() => status.refetch()} />}
            {status.isPending && <div className={CK}><Skeleton w="70%" /></div>}
            {result?.checks.map((c) => (
              <div className={CK} key={c.key}>
                <span className={'grid size-5 shrink-0 place-items-center rounded-full ' + (c.ok ? 'bg-dns text-dnt' : 'bg-rds text-rd')}><Icon name={c.ok ? 'check' : 'x'} size={12} sw={3} /></span>
                <span className="flex-[0_0_160px] text-sm font-medium">{tr(c.label)}</span>
                <span className="min-w-0 flex-[1_1_180px] font-mono text-[12px] text-t3 [overflow-wrap:anywhere]">{detail(c.detail)}</span>
                <span className="font-mono text-[12px] text-t2">{c.ok ? unit(c.value) || tr('ок') : tr('ошибка')}</span>
                {!c.ok && c.hint && <span className="basis-full pl-8 text-[12.5px] text-rd">{tr('Что делать: {hint}', { hint: tr(c.hint) })}</span>}
              </div>
            ))}
            {check.isError && <div className={ERR_PAD} role="alert">{check.error.message}</div>}
          </section>

          <section className={PANEL}>
            <div className={PH_ROW + ' pb-3'}>
              <h2 className={PH_H2}>{tr('Подключение')}</h2>
              <span className={SEG} role="group" aria-label={tr('Режим доступа')}>
                {([['rw', 'Чтение и запись'], ['ro', 'Только чтение']] as const).map(([k, n]) => (
                  <button key={k} type="button" className={SEGBTN + 'aria-pressed:bg-sk aria-pressed:font-semibold aria-pressed:text-tx'} aria-pressed={s?.access_mode === k} disabled={!s || patch.isPending} onClick={() => patch.mutate({ access_mode: k })}>{tr(n)}</button>
                ))}
              </span>
            </div>
            <div className="flex flex-col gap-4 px-5 pb-5">
              <div className="grid grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-4">
                <div className={LB}>{tr('Адрес сервера')}
                  <div className={FLD}><span className="flex-1 truncate font-mono">{address}</span>
                    <button type="button" className={BTN_SM_SQ} aria-label={tr('Копировать адрес')} onClick={() => copyText(address)?.then(() => flash('addr'))}><Icon name={copied === 'addr' ? 'check' : 'link'} size={15} /></button>
                  </div>
                  <span className={HINT}>{tr('Транспорт: Streamable HTTP. Слушает только 127.0.0.1.')}</span>
                </div>
                <div className={LB}>{tr('Токен')}
                  <div className={FLD}><Icon name="key" size={15} /><span className="flex-1 truncate font-mono">{shownToken}</span>
                    <button type="button" className={BTN_SM_SQ} aria-label={token ? tr('Скрыть токен') : tr('Показать токен')} onClick={() => (token ? setToken(null) : reveal.mutate())}><Icon name={token ? 'x' : 'search'} size={15} /></button>
                    <button type="button" className={BTN_SM_SQ} aria-label={tr('Копировать токен')} onClick={async () => { await copyText(await fullToken()); flash('token') }}><Icon name={copied === 'token' ? 'check' : 'link'} size={15} /></button>
                  </div>
                  <span className={HINT}>{s?.token_from_env ? tr('Задан в .env как DOCKET_MCP_TOKEN: перевыпуск отключён.') : tr('Хранится в базе. После перевыпуска обновите клиент.')}</span>
                </div>
              </div>
              <div>
                <div className="mb-2.5 flex flex-wrap items-center gap-3">
                  <span className={SEG} role="tablist" aria-label={tr('Клиент')}>
                    {CODE_TAB.map((n, i) => <button key={n} type="button" role="tab" aria-selected={tab === i} className={SEGBTN + 'aria-selected:bg-sk aria-selected:font-semibold aria-selected:text-tx'} onClick={() => setTab(i)}>{n}</button>)}
                  </span>
                  <span className="grow" />
                  <button type="button" className={BTN_SM} onClick={async () => { await copyText(config(tab, address, await fullToken())); flash('cfg') }}>
                    <Icon name={copied === 'cfg' ? 'check' : 'doc'} size={14} />{copied === 'cfg' ? tr('Скопировано') : tr('Скопировать конфиг')}
                  </button>
                  <ConfirmButton className={BTN_SM + ' [&.warn]:border-rd [&.warn]:text-rd'} label={tr('Перевыпустить токен')} armed={tr('Старый токен перестанет работать. Перевыпустить?')} disabled={!s || s.token_from_env || rotate.isPending}
                    icon={<Icon name="clock" size={14} />} onConfirm={() => rotate.mutate()} />
                </div>
                {rotated && <div className="mb-2 text-[13px] text-dgt" role="status">{tr('Токен перевыпущен. Обновите его в клиенте: скопируйте конфиг ниже.')}</div>}
                <pre className="overflow-x-auto rounded-[10px] border border-ln bg-sf2 px-4 py-3.5 font-mono text-[12.5px] leading-[1.65] whitespace-pre text-t2">{config(tab, address, shownToken)}</pre>
                {rotate.isError && <div className="text-[13px] text-rd" role="alert">{rotate.error.message}</div>}
              </div>
            </div>
          </section>

          <McpAgent tab={tab} />

          <section className={PANEL}>
            <div className={PH_ROW + ' pb-3'}><h2 className={PH_H2}>{tr('Инструменты')}</h2><span className="text-[13px] text-t3">{tr('{n} из {total} включено', { n: enabled, total: s?.tools.length ?? 0 })}</span></div>
            {settings.isError && <ErrorState message={settings.error.message} onRetry={() => settings.refetch()} />}
            {settings.isPending && <div className={TL}><Skeleton w="70%" /></div>}
            {groups.map(([name, tools]) => (
              <div key={name}>
                <div className="border-t border-ln bg-sf2 px-5 pt-2 pb-1.5 text-xs font-medium text-t3">{tr(name)}{name === 'Запись' && readOnly && tr(' · недоступна в режиме «Только чтение»')}</div>
                {tools.map((t) => {
                  const off = name === 'Запись' && readOnly
                  const dim = off ? ' opacity-[.55]' : ''
                  return (
                    <div className={TL} key={t.name}>
                      <span className={'flex-[0_0_150px] font-mono text-[12.5px] font-medium' + dim}>{t.name}</span>
                      <span className={'min-w-0 flex-[1_1_200px] text-[13px] text-t2' + dim}>{t.description}</span>
                      <span className="flex-[0_0_52px] text-right font-mono text-[12px] tabular-nums text-t3" title={tr('Вызовы за 7 дней')}>{t.calls_7d}</span>
                      <Toggle on={t.enabled} disabled={off || patch.isPending} label={tr('Инструмент {name}', { name: t.name })} onChange={(v) => patch.mutate({ tools: { [t.name]: v } })} />
                    </div>
                  )
                })}
              </div>
            ))}
            <div className="border-t border-ln px-5 py-3 text-[12.5px] text-t3">{tr('Число справа: вызовы за 7 дней. Выключенный инструмент пропадает из списка, который видит Claude.')}</div>
            {patch.isError && <div className={ERR_PAD} role="alert">{patch.error.message}</div>}
          </section>
        </div>

        <aside className="min-w-0 max-w-[480px] flex-[1_1_380px]">
          <section className={PANEL}>
            <div className={PH_ROW + ' pb-2.5'}>
              <h2 className={PH_H2}>{tr('Журнал вызовов')}</h2>
              <span className="inline-flex items-center gap-1.5 font-mono text-[11px] leading-[normal] font-semibold tracking-[.06em] text-dnt"><span className="inline-block size-[7px] shrink-0 rounded-full bg-dn" />LIVE</span>
            </div>
            <div className="flex flex-wrap items-center gap-2 px-[18px] pb-3">
              {([['all', tr('Все')], ['write', tr('Запись')], ['error', tr('Ошибки: {n}', { n: errors.data?.length ?? 0 })]] as const).map(([k, n]) => (
                <button key={k} type="button" className={FC} aria-pressed={filter === k} onClick={() => setFilter(k)}>{n}</button>
              ))}
              <span className="grow" />
              {st && <span className={MONO}>{tr('{calls} сегодня · медиана {ms} мс', { calls: st.calls_today, ms: st.median_ms })}</span>}
            </div>
            {calls.isError && <ErrorState message={calls.error.message} onRetry={() => calls.refetch()} />}
            {calls.isPending && <div className={'grid grid-cols-[62px_20px_minmax(0,1fr)_auto] ' + LG}><Skeleton w={40} /><span /><Skeleton /><span /></div>}
            {calls.isSuccess && calls.data.length === 0 && <div className={'block text-t3 ' + LG}>{tr('Вызовов пока нет. Они появятся здесь, как только Claude обратится к Docket.')}</div>}
            {calls.data?.map((c) => (
              <div className={'grid grid-cols-[62px_20px_minmax(0,1fr)_auto] ' + LG} key={c.id}>
                <span className="font-mono text-t3">{new Date(c.at).toLocaleTimeString(locale())}</span>
                <span className="grid size-5 place-items-center rounded-full bg-acs text-act" title={c.actor}><Icon name="spark" size={11} sw={2.2} /></span>
                <span className="flex min-w-0 flex-col gap-px">
                  <span className="font-mono text-[12.5px] font-medium">{c.tool}</span>
                  <span className={MONO + ' truncate'} title={c.args_summary}>{c.args_summary}</span>
                </span>
                <span className="flex items-center gap-2">
                  <span className={MONO}>{c.duration_ms} {tr('мс')}</span>
                  <span className={'rounded-[5px] px-[7px] py-0.5 font-mono text-[11px] leading-[normal] font-semibold ' + (c.status === 'ok' ? 'bg-dns text-dnt' : 'bg-rds text-rd')} title={c.error_code ?? undefined}>{c.status === 'ok' ? 'ok' : tr('ошибка')}</span>
                </span>
              </div>
            ))}
            <div className="flex items-center border-t border-ln px-[18px] py-3 text-[12.5px] text-t3">
              <span className="grow">{tr('Хранится 7 дней')}</span>
              <ConfirmButton className="p-0 text-[12.5px] text-t3 hover:text-tx" label={tr('Очистить журнал')} armed={tr('Точно очистить?')} disabled={clear.isPending || !calls.data?.length} onConfirm={() => clear.mutate()} />
            </div>
          </section>
        </aside>
      </div>
    </>
  )
}
