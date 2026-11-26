import { OAuthConnections } from './OAuth'
// Integrations tab: the ways an assistant reaches Plandillo (MCP, Code Mode) and what they share: status and
// self-check, token and access mode, tool switches, live call log. See docs/integrations.md.
// Top: status strip (checks folded unless one failed). Main column: connect, apps, access, tools (folded). Side column: the log.
import { Fragment, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type McpTool, agentName, api, dayHead, fmtStamp, isoDay, mcpHealthLabel } from './api'
import { Icon } from './icons'
import { locale, tr } from './i18n'
import { McpAgent } from './McpAgent'
import { ConfirmButton, ErrorState, Skeleton, Toggle, cap } from './ui'

const BTN_LOOK = 'items-center gap-2 whitespace-nowrap rounded-md border font-medium no-underline disabled:cursor-default disabled:opacity-50'
const BTN_SM = 'inline-flex h-8 px-2.5 text-[13px] border-ln bg-sf text-tx hover:bg-sk ' + BTN_LOOK
const BTN_SQ = 'inline-flex size-7 justify-center p-0 text-[13px] border-transparent bg-transparent text-t2 hover:bg-sk hover:text-tx ' + BTN_LOOK
const PANEL = 'overflow-hidden rounded-xl border border-ln bg-sf'
const HEAD = 'flex flex-wrap items-center gap-2 px-4 pt-3.5 pb-3'
const H2 = 'mr-auto text-[15px] font-semibold'
const ERR_PAD = 'px-4 pb-3 text-[13px] text-rd'
const ROW = 'flex items-center gap-3 border-t border-ln px-4 py-2'
const LG = 'items-center gap-2.5 border-t border-ln px-4 py-2 text-[12.5px]'
const SEG = 'inline-flex gap-0.5 rounded-md border border-ln p-0.5 text-[12.5px]'
const SEGBTN = 'rounded-sm px-2.5 py-[3px] text-t3 disabled:cursor-default disabled:opacity-50 aria-pressed:bg-sk aria-pressed:font-semibold aria-pressed:text-tx'
const FC = 'inline-flex h-7 items-center gap-1.5 whitespace-nowrap rounded-full border border-ln bg-sf px-2.5 text-[12.5px] text-t2 hover:bg-sk aria-pressed:border-transparent aria-pressed:bg-acs aria-pressed:font-medium aria-pressed:text-act aria-pressed:hover:bg-sk'
const MONO = 'font-mono text-[11.5px] text-t3'
// A <details> whose summary is a panel header: no browser marker, a chevron that turns when open.
const SUMMARY = 'flex cursor-pointer list-none items-center gap-2 px-4 py-3 [&::-webkit-details-marker]:hidden'
const CHEVRON = 'text-t3 motion-safe:transition-transform group-open:rotate-180'
// The server writes its check texts in Russian. Fixed parts go through tr(); a trailing ": value" or timestamp stays as is.
const detail = (s: string) => s.split(' · ').map((p) => {
  const m = p.match(/^(.*?)(: .*| \d\d\.\d\d \d\d:\d\d)$/)
  return m ? tr(m[1]) + m[2] : tr(p)
}).join(' · ')
const unit = (v: string) => v.replace(/ (мс|МБ)$/, (_m, u: string) => ' ' + tr(u))

function copyText(text: string) {
  return navigator.clipboard?.writeText(text)
}

export function Mcp() {
  const qc = useQueryClient()
  const status = useQuery({ queryKey: ['mcp-status'], queryFn: api.mcpStatus })
  const settings = useQuery({ queryKey: ['mcp-settings'], queryFn: api.mcpSettings })
  const [filter, setFilter] = useState<'all' | 'write' | 'error'>('all')
  const calls = useQuery({ queryKey: ['mcp-calls', filter], queryFn: () => api.mcpCalls(filter) })
  const errors = useQuery({ queryKey: ['mcp-calls', 'error'], queryFn: () => api.mcpCalls('error') })

  const check = useMutation({
    mutationFn: api.mcpCheck,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['mcp-status'] }),
  })
  const patch = useMutation({
    mutationFn: api.patchMcpSettings,
    onSuccess: (d) => qc.setQueryData(['mcp-settings'], d),
    onSettled: () => qc.invalidateQueries({ queryKey: ['mcp-settings'] }),
  })

  const [token, setToken] = useState<string | null>(null) // full token once revealed or rotated
  const [rotated, setRotated] = useState(false)
  const reveal = useMutation({ mutationFn: api.mcpToken, onSuccess: (t) => setToken(t.token) })
  const rotate = useMutation({
    mutationFn: api.rotateMcpToken,
    onSuccess: (t) => { setToken(t.token); setRotated(true); qc.invalidateQueries({ queryKey: ['mcp-settings'] }); qc.invalidateQueries({ queryKey: ['oauth-connections'] }) },
  })
  const clear = useMutation({ mutationFn: api.clearMcpCalls, onSettled: () => qc.invalidateQueries({ queryKey: ['mcp-calls'] }) })
  const [copied, setCopied] = useState('')
  const flash = (what: string) => { setCopied(what); setTimeout(() => setCopied(''), 1500) }
  const fullToken = async () => token ?? (await api.mcpToken()).token

  const s = settings.data
  const st = status.data
  const result = st?.check
  const online = !!st?.online
  const failed = result?.checks.filter((c) => !c.ok).length ?? 0
  const enabled = s?.tools.filter((t) => t.enabled).length ?? 0
  const masked = s?.token_masked ?? 'dkt_••••'
  const shownToken = token ?? masked
  const kind = new Map(s?.tools.map((t) => [t.name, t.group]))
  // shortcut: the log API returns at most 50 calls, so a full page reads "50+"; add a count endpoint if the exact number matters.
  const errCount = errors.data ? (errors.data.length >= 50 ? '50+' : String(errors.data.length)) : '—'

  const groups: [string, McpTool[]][] = [
    ['Чтение', (s?.tools ?? []).filter((t) => t.group === 'read')],
    ['Запись', (s?.tools ?? []).filter((t) => t.group === 'write')],
  ]
  const readOnly = s?.access_mode === 'ro'
  const patchError = patch.isError && <div className={ERR_PAD} role="alert">{patch.error.message}</div>

  return (
    <>
      <header className="flex items-center gap-3 px-6 pt-5 pb-4 max-[640px]:px-4">
        <span className="grid size-9 shrink-0 place-items-center rounded-[10px] bg-sk"><Icon name="plug" size={18} sw={1.9} /></span>
        <div className="flex min-w-0 flex-col">
          <h1 className="text-xl font-bold tracking-tight">{tr('Интеграции')}</h1>
          <span className="text-[13px] text-t3">{tr('Подключите ассистента к задачам и страницам вашего пространства.')}</span>
        </div>
      </header>

      <section aria-label={tr('Состояние MCP-сервера')} className={PANEL + ' mx-6 mb-4 max-[640px]:mx-4'}>
        <div className="flex flex-wrap items-center gap-x-7 gap-y-3 px-4 py-3.5">
          <div className="flex min-w-0 flex-[1_1_220px] items-center gap-3">
            <span className={'grid size-8 shrink-0 place-items-center rounded-full ' + (!result || status.isError ? 'bg-sk text-t3' : online ? 'bg-dns text-dnt' : 'bg-rds text-rd')}>
              <Icon name={!result || status.isError ? 'plug' : online ? 'check' : 'x'} size={16} sw={2.4} />
            </span>
            <div className="flex min-w-0 flex-col">
              <span className="text-sm font-semibold" role="status">{mcpHealthLabel(st, status.isError)}</span>
              <span className="truncate font-mono text-xs text-t3" dir="ltr">{location.host}/mcp</span>
            </div>
          </div>
          <dl className="flex flex-wrap gap-x-7 gap-y-2 text-[13px]">
            {result && <div><dt className="text-xs text-t3">{tr('Проверка')}</dt><dd className="tabular-nums">{tr('{when}, {ok} из {n} успешно', { when: fmtStamp(result.at), ok: result.checks.length - failed, n: result.checks.length })}</dd></div>}
            <div><dt className="text-xs text-t3">{tr('Последний вызов')}</dt><dd className="tabular-nums">{st?.last_call_at ? fmtStamp(st.last_call_at) : '—'}</dd></div>
            <div><dt className="text-xs text-t3">{tr('Ошибок за 7 дней')}</dt><dd className={'tabular-nums ' + (errors.data?.length ? 'text-rd' : '')}>{errCount}</dd></div>
          </dl>
          <button type="button" className={BTN_SM} disabled={check.isPending} onClick={() => check.mutate()}>
            <Icon name="clock" size={14} />{check.isPending ? tr('Проверяю…') : tr('Проверить снова')}
          </button>
        </div>
        {status.isError && <ErrorState error={status.error} onRetry={() => status.refetch()} />}
        {status.isPending && <div className={ROW}><Skeleton w="70%" /></div>}
        {result && (
          <details className="group border-t border-ln" open={failed > 0}>
            <summary className={SUMMARY + ' py-2 text-[12.5px] text-t2'}>
              <span className="mr-auto">{tr('Результаты проверки')}</span>
              <span className={CHEVRON}><Icon name="chevron" size={15} /></span>
            </summary>
            {result.checks.map((c) => (
              <div className="grid grid-cols-[20px_minmax(0,1fr)_auto] items-center gap-x-2.5 gap-y-0.5 border-t border-ln px-4 py-2 text-[13px]" key={c.key}>
                <span className={'grid size-5 place-items-center rounded-full ' + (c.ok ? 'bg-dns text-dnt' : 'bg-rds text-rd')}><Icon name={c.ok ? 'check' : 'x'} size={12} sw={3} /></span>
                <span className="font-medium">{tr(c.label)}</span>
                <span className="font-mono text-[12px] text-t2">{c.ok ? unit(c.value) || tr('ок') : tr('ошибка')}</span>
                <span className="col-start-2 col-end-4 font-mono text-[11.5px] text-t3 [overflow-wrap:anywhere]">{detail(c.detail)}</span>
                {!c.ok && c.hint && <span className="col-start-2 col-end-4 text-[12.5px] text-rd">{tr('Что делать: {hint}', { hint: tr(c.hint) })}</span>}
              </div>
            ))}
          </details>
        )}
        {check.isError && <div className={ERR_PAD} role="alert">{check.error.message}</div>}
      </section>

      <div className="flex flex-wrap items-start gap-4 px-6 pb-8 max-[640px]:px-4">
        <div className="flex min-w-0 flex-[999_1_500px] flex-col gap-4">
          <McpAgent />

          <OAuthConnections />

          <section className={PANEL}>
            <div className={HEAD}>
              <h2 className={H2}>{tr('Доступ')}</h2>
              <span className={SEG} role="group" aria-label={tr('Режим доступа')}>
                {([['rw', 'Чтение и запись'], ['ro', 'Только чтение']] as const).map(([k, n]) => (
                  <button key={k} type="button" className={SEGBTN} aria-pressed={s?.access_mode === k} disabled={!s || patch.isPending} onClick={() => patch.mutate({ access_mode: k })}>{tr(n)}</button>
                ))}
              </span>
            </div>
            <div className="flex flex-col gap-1.5 px-4 pb-4">
              <span className="text-[12.5px] font-medium text-t2">{tr('Токен для скриптов')}</span>
              <div className="flex flex-wrap items-center gap-2">
                <div className="flex h-8 min-w-0 flex-[1_1_240px] items-center gap-2 rounded-md border border-ln bg-sf2 pr-0.5 pl-2.5 text-[13px]">
                  <Icon name="key" size={14} /><span className="flex-1 truncate font-mono text-[12.5px]">{shownToken}</span>
                  <button type="button" className={BTN_SQ} aria-label={token ? tr('Скрыть токен') : tr('Показать токен')} title={token ? tr('Скрыть токен') : tr('Показать токен')} onClick={() => (token ? setToken(null) : reveal.mutate())}><Icon name={token ? 'x' : 'search'} size={14} /></button>
                  <button type="button" className={BTN_SQ} aria-label={tr('Копировать токен')} title={tr('Копировать токен')} onClick={async () => { await copyText(await fullToken()); flash('token') }}><Icon name={copied === 'token' ? 'check' : 'link'} size={14} /></button>
                </div>
                <ConfirmButton className={BTN_SM + ' [&.warn]:border-rd [&.warn]:text-rd'} label={tr('Перевыпустить токен')} armed={tr('Перевыпуск отключит токен и приложения этого пространства. Продолжить?')} disabled={!s || s.token_from_env || rotate.isPending}
                  icon={<Icon name="clock" size={14} />} onConfirm={() => rotate.mutate()} />
              </div>
              <span className="text-xs text-t3">{s?.token_from_env ? tr('Задан в .env как PLANDILLO_MCP_TOKEN: перевыпуск отключён.') : tr('Для входа через браузер достаточно адреса сервера.')}</span>
              {rotated && <div className="text-[13px] text-dgt" role="status">{tr('Токен перевыпущен. Скопируйте новый токен и обновите его в клиенте.')}</div>}
              {rotate.isError && <div className="text-[13px] text-rd" role="alert">{rotate.error.message}</div>}
            </div>
            {patchError}
          </section>

          <details className={PANEL + ' group'}>
            <summary className={SUMMARY}>
              <h2 className={H2}>{tr('Инструменты')}</h2>
              <span className="text-[13px] text-t3">{tr('{n} из {total} включено', { n: enabled, total: s?.tools.length ?? 0 })}</span>
              <span className={CHEVRON}><Icon name="chevron" size={16} /></span>
            </summary>
            {settings.isError && <ErrorState error={settings.error} onRetry={() => settings.refetch()} />}
            {settings.isPending && <div className={ROW}><Skeleton w="70%" /></div>}
            {groups.map(([name, tools]) => (
              <div key={name}>
                <div className="border-t border-ln bg-sf2 px-4 pt-2 pb-1.5 text-xs font-medium text-t3">{tr(name)}{name === 'Запись' && readOnly && tr(' · недоступна в режиме «Только чтение»')}</div>
                {tools.map((t) => {
                  const off = name === 'Запись' && readOnly
                  const dim = off ? ' opacity-[.55]' : ''
                  return (
                    <div className={ROW} key={t.name}>
                      <span className={'flex-[0_0_140px] font-mono text-[12.5px] font-medium' + dim}>{t.name}</span>
                      <span className={'min-w-0 flex-[1_1_200px] text-[13px] text-t2' + dim}>{t.description}</span>
                      <span className="flex-[0_0_40px] text-right font-mono text-[12px] tabular-nums text-t3" title={tr('Вызовы за 7 дней')}>{t.calls_7d}</span>
                      <Toggle on={t.enabled} disabled={off || patch.isPending} label={tr('Инструмент {name}', { name: t.name })} onChange={(v) => patch.mutate({ tools: { [t.name]: v } })} />
                    </div>
                  )
                })}
              </div>
            ))}
            <div className="border-t border-ln px-4 py-2.5 text-xs text-t3">{tr('Число справа: вызовы за 7 дней. Выключенный инструмент пропадает из списка, который видит агент, и из codemode в Code Mode.')}</div>
            {patchError}
          </details>
        </div>

        <aside className="flex min-w-0 flex-[1_1_400px] flex-col gap-4">
          <section className={PANEL}>
            <div className={HEAD + ' pb-2'}>
              <h2 className={H2}>{tr('Журнал вызовов')}</h2>
              {st && <span className={MONO}>{tr('{calls} сегодня · медиана {ms} мс', { calls: st.calls_today, ms: st.median_ms })}</span>}
            </div>
            <div className="flex flex-wrap items-center gap-1.5 px-4 pb-3">
              {([['all', tr('Все')], ['write', tr('Запись')], ['error', tr('Ошибки: {n}', { n: errors.data?.length ?? 0 })]] as const).map(([k, n]) => (
                <button key={k} type="button" className={FC} aria-pressed={filter === k} onClick={() => setFilter(k)}>{n}</button>
              ))}
            </div>
            {calls.isError && <ErrorState error={calls.error} onRetry={() => calls.refetch()} />}
            {calls.isPending && <div className={'grid grid-cols-[62px_minmax(0,1fr)_auto] ' + LG}><Skeleton w={40} /><Skeleton /><span /></div>}
            {calls.isSuccess && calls.data.length === 0 && <div className={'block text-t3 ' + LG}>{filter === 'all' ? tr('В журнале пока нет вызовов') : tr('Вызовов по этому фильтру нет')}</div>}
            <div className="max-h-[55vh] overflow-y-auto">
              {calls.data?.map((c, i) => {
                const day = isoDay(new Date(c.at))
                const group = kind.get(c.tool)
                return (
                  <Fragment key={c.id}>
                    {(i === 0 || isoDay(new Date(calls.data[i - 1].at)) !== day) && <div className={cap + ' border-t border-ln bg-sf2 px-4 py-1.5'}>{dayHead(day)}</div>}
                    <div className={'grid grid-cols-[62px_minmax(0,1fr)_auto] gap-y-px ' + LG}>
                      <span className="font-mono text-t3">{new Date(c.at).toLocaleTimeString(locale())}</span>
                      <span className="flex min-w-0 items-center gap-2">
                        <span className="truncate font-mono text-[12.5px] font-medium">{c.tool}</span>
                        {group && <span className={'text-[11.5px] ' + (group === 'write' ? 'text-act' : 'text-t3')}>{group === 'write' ? tr('запись') : tr('чтение')}</span>}
                      </span>
                      <span className="flex items-center gap-2">
                        <span className={MONO}>{c.duration_ms} {tr('мс')}</span>
                        <span className={'rounded-[5px] px-[7px] py-0.5 font-mono text-[11px] leading-[normal] font-semibold ' + (c.status === 'ok' ? 'bg-dns text-dnt' : 'bg-rds text-rd')} title={c.error_code ?? undefined}>{c.status === 'ok' ? 'ok' : tr('ошибка')}</span>
                      </span>
                      <span className="col-start-2 col-end-4 truncate text-xs text-t2" title={c.args_summary}>{agentName(c.actor)}{c.args_summary && <span className="ml-2 font-mono text-[11.5px] text-t3">{c.args_summary}</span>}</span>
                    </div>
                  </Fragment>
                )
              })}
            </div>
            <div className="flex items-center border-t border-ln px-4 py-2.5 text-[12.5px] text-t3">
              <span className="grow">{tr('Хранится 7 дней')}</span>
              <ConfirmButton className="p-0 text-[12.5px] text-t3 hover:text-tx" label={tr('Очистить журнал')} armed={tr('Точно очистить?')} disabled={clear.isPending || !calls.data?.length} onConfirm={() => clear.mutate()} />
            </div>
          </section>
        </aside>
      </div>
    </>
  )
}
