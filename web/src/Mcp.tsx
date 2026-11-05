// MCP tab: connection status and self-check, connection settings and token, tool switches, live call log.
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type McpTool, api, fmtTime } from './api'
import { Icon } from './icons'
import { ConfirmButton, ErrorState, Skeleton, Toggle } from './ui'

const CODE_TAB = ['Claude Code', 'Claude Desktop'] as const

function copyText(text: string) {
  return navigator.clipboard?.writeText(text)
}

function config(tab: number, address: string, token: string) {
  if (tab === 0) return `claude mcp add --transport http docket ${address} \\\n  --header "Authorization: Bearer ${token}"`
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
      <header className="pagehead">
        <span style={{ width: 40, height: 40, borderRadius: 11, background: 'var(--sk)', display: 'grid', placeItems: 'center' }}><Icon name="plug" size={20} sw={1.9} /></span>
        <div className="stack">
          <h1 style={{ fontSize: 24 }}>MCP</h1>
          <span className="sub">Так Claude читает и ведёт задачи. Сервер работает внутри Docket, отдельно ничего ставить не нужно.</span>
        </div>
        {st && (
          <span className={'pill' + (online ? '' : ' bad')} role="status">
            <span className="dot" style={{ background: online ? 'var(--dn)' : 'var(--rd)' }} />
            {online ? `Онлайн · ${result?.checks.find((c) => c.key === 'server')?.value ?? ''}` : 'Недоступен'}
          </span>
        )}
      </header>

      <div className="mcpgrid">
        <div className="col-main" style={{ flex: '999 1 540px', gap: 20 }}>
          <section className="panel clip">
            <div style={{ display: 'flex', alignItems: 'center', gap: 16, flexWrap: 'wrap', padding: 20 }}>
              <span style={{ width: 46, height: 46, borderRadius: '50%', display: 'grid', placeItems: 'center', flex: '0 0 46px', background: online || !result ? 'var(--dns)' : 'var(--rds)', color: online || !result ? 'var(--dnt)' : 'var(--rd)' }}>
                <Icon name={online || !result ? 'check' : 'x'} size={22} sw={2.2} />
              </span>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 2, flex: '1 1 220px', minWidth: 0 }}>
                <span style={{ fontSize: 19, fontWeight: 700, letterSpacing: '-.01em' }}>{!result ? 'Проверка…' : online ? 'Подключено' : 'Есть проблемы'}</span>
                <span style={{ fontSize: 13, color: 'var(--t3)' }}>
                  {st?.last_call_at ? <>Последний вызов от <b style={{ fontWeight: 600, color: 'var(--t2)' }}>claude</b> в {fmtTime(st.last_call_at)}. </> : 'Вызовов ещё не было. '}
                  {result && `Проверка: ${checked ? 'только что' : fmtTime(result.at)}, ${result.checks.filter((c) => c.ok).length} из ${result.checks.length}.`}
                </span>
              </div>
              <button type="button" className="btn pri" disabled={check.isPending} onClick={() => check.mutate()}>
                <Icon name="clock" size={15} />{check.isPending ? 'Проверяю…' : checked ? 'Проверено только что' : 'Проверить соединение'}
              </button>
            </div>
            {status.isError && <ErrorState message={status.error.message} onRetry={() => status.refetch()} />}
            {status.isPending && <div className="ck"><Skeleton w="70%" /></div>}
            {result?.checks.map((c) => (
              <div className="ck" key={c.key}>
                <span className={'ckicon' + (c.ok ? '' : ' bad')}><Icon name={c.ok ? 'check' : 'x'} size={12} sw={3} /></span>
                <span style={{ flex: '0 0 160px', fontWeight: 500, fontSize: 13.5 }}>{c.label}</span>
                <span className="mono" style={{ flex: '1 1 180px', minWidth: 0, fontSize: 12, color: 'var(--t3)', overflowWrap: 'anywhere' }}>{c.detail}</span>
                <span className="mono" style={{ fontSize: 12, color: 'var(--t2)' }}>{c.ok ? c.value || 'ок' : 'ошибка'}</span>
                {!c.ok && c.hint && <span style={{ flexBasis: '100%', paddingLeft: 32, fontSize: 12.5, color: 'var(--rd)' }}>Что делать: {c.hint}</span>}
              </div>
            ))}
            {check.isError && <div className="errline" style={{ padding: '0 20px 14px' }} role="alert">{check.error.message}</div>}
          </section>

          <section className="panel clip">
            <div className="ph row">
              <h2 className="grow">Подключение</h2>
              <span className="segmini" role="group" aria-label="Режим доступа">
                {([['rw', 'Чтение и запись'], ['ro', 'Только чтение']] as const).map(([k, n]) => (
                  <button key={k} type="button" className={s?.access_mode === k ? 'on' : ''} aria-pressed={s?.access_mode === k} disabled={!s || patch.isPending} onClick={() => patch.mutate({ access_mode: k })}>{n}</button>
                ))}
              </span>
            </div>
            <div className="pb" style={{ paddingTop: 0, gap: 16 }}>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 16 }}>
                <div className="lb">Адрес сервера
                  <div className="fld"><span className="mono">{address}</span>
                    <button type="button" className="btn sm sq" aria-label="Копировать адрес" onClick={() => copyText(address)?.then(() => flash('addr'))}><Icon name={copied === 'addr' ? 'check' : 'link'} size={15} /></button>
                  </div>
                  <span className="hint">Транспорт: Streamable HTTP. Слушает только 127.0.0.1.</span>
                </div>
                <div className="lb">Токен
                  <div className="fld"><Icon name="key" size={15} /><span className="mono">{shownToken}</span>
                    <button type="button" className="btn sm sq" aria-label={token ? 'Скрыть токен' : 'Показать токен'} onClick={() => (token ? setToken(null) : reveal.mutate())}><Icon name={token ? 'x' : 'search'} size={15} /></button>
                    <button type="button" className="btn sm sq" aria-label="Копировать токен" onClick={async () => { await copyText(await fullToken()); flash('token') }}><Icon name={copied === 'token' ? 'check' : 'link'} size={15} /></button>
                  </div>
                  <span className="hint">{s?.token_from_env ? 'Задан в .env как DOCKET_MCP_TOKEN: перевыпуск отключён.' : 'Хранится в базе. После перевыпуска обновите клиент.'}</span>
                </div>
              </div>
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 10, flexWrap: 'wrap' }}>
                  <span className="segmini" role="tablist" aria-label="Клиент">
                    {CODE_TAB.map((n, i) => <button key={n} type="button" role="tab" aria-selected={tab === i} className={tab === i ? 'on' : ''} onClick={() => setTab(i)}>{n}</button>)}
                  </span>
                  <span className="grow" />
                  <button type="button" className="btn sm" onClick={async () => { await copyText(config(tab, address, await fullToken())); flash('cfg') }}>
                    <Icon name={copied === 'cfg' ? 'check' : 'doc'} size={14} />{copied === 'cfg' ? 'Скопировано' : 'Скопировать конфиг'}
                  </button>
                  <ConfirmButton className="btn sm" label="Перевыпустить токен" armed="Старый токен перестанет работать. Перевыпустить?" disabled={!s || s.token_from_env || rotate.isPending}
                    icon={<Icon name="clock" size={14} />} onConfirm={() => rotate.mutate()} />
                </div>
                {rotated && <div className="errline" style={{ color: 'var(--dgt)', marginBottom: 8 }} role="status">Токен перевыпущен. Обновите его в клиенте: скопируйте конфиг ниже.</div>}
                <pre className="codeblock mono">{config(tab, address, shownToken)}</pre>
                {rotate.isError && <div className="errline" role="alert">{rotate.error.message}</div>}
              </div>
            </div>
          </section>

          <section className="panel clip">
            <div className="ph row"><h2 className="grow">Инструменты</h2><span style={{ fontSize: 13, color: 'var(--t3)' }}>{enabled} из {s?.tools.length ?? 0} включено</span></div>
            {settings.isError && <ErrorState message={settings.error.message} onRetry={() => settings.refetch()} />}
            {settings.isPending && <div className="tl"><Skeleton w="70%" /></div>}
            {groups.map(([name, tools]) => (
              <div key={name}>
                <div className="cap" style={{ padding: '8px 20px 6px', borderTop: '1px solid var(--ln)', background: 'var(--sf2)' }}>{name}{name === 'Запись' && readOnly && ' · недоступна в режиме «Только чтение»'}</div>
                {tools.map((t) => {
                  const off = name === 'Запись' && readOnly
                  return (
                    <div className={'tl' + (off ? ' off' : '')} key={t.name}>
                      <span className="mono name" style={{ flex: '0 0 150px', fontSize: 12.5, fontWeight: 500 }}>{t.name}</span>
                      <span className="desc" style={{ flex: '1 1 200px', minWidth: 0, fontSize: 13, color: 'var(--t2)' }}>{t.description}</span>
                      <span className="mono tabnum" style={{ fontSize: 12, color: 'var(--t3)', flex: '0 0 52px', textAlign: 'right' }} title="Вызовы за 7 дней">{t.calls_7d}</span>
                      <Toggle on={t.enabled} disabled={off || patch.isPending} label={`Инструмент ${t.name}`} onChange={(v) => patch.mutate({ tools: { [t.name]: v } })} />
                    </div>
                  )
                })}
              </div>
            ))}
            <div style={{ padding: '12px 20px', borderTop: '1px solid var(--ln)', fontSize: 12.5, color: 'var(--t3)' }}>Число справа: вызовы за 7 дней. Выключенный инструмент пропадает из списка, который видит Claude.</div>
            {patch.isError && <div className="errline" style={{ padding: '0 20px 14px' }} role="alert">{patch.error.message}</div>}
          </section>
        </div>

        <aside style={{ flex: '1 1 380px', maxWidth: 480, minWidth: 0 }}>
          <section className="panel clip">
            <div className="ph row" style={{ paddingBottom: 10 }}>
              <h2 className="grow">Журнал вызовов</h2>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, font: "600 11px 'JetBrains Mono', monospace", color: 'var(--dnt)', letterSpacing: '.06em' }}><span className="dot" style={{ width: 7, height: 7, flexBasis: 7, background: 'var(--dn)' }} />LIVE</span>
            </div>
            <div style={{ padding: '0 18px 12px', display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
              {([['all', 'Все'], ['write', 'Запись'], ['error', `Ошибки: ${errors.data?.length ?? 0}`]] as const).map(([k, n]) => (
                <button key={k} type="button" className={'fc' + (filter === k ? ' on' : '')} aria-pressed={filter === k} onClick={() => setFilter(k)}>{n}</button>
              ))}
              <span className="grow" />
              {st && <span className="mono" style={{ fontSize: 11.5, color: 'var(--t3)' }}>{st.calls_today} сегодня · медиана {st.median_ms} мс</span>}
            </div>
            {calls.isError && <ErrorState message={calls.error.message} onRetry={() => calls.refetch()} />}
            {calls.isPending && <div className="lg"><Skeleton w={40} /><span /><Skeleton /><span /></div>}
            {calls.isSuccess && calls.data.length === 0 && <div className="lg" style={{ display: 'block', color: 'var(--t3)' }}>Вызовов пока нет. Они появятся здесь, как только Claude обратится к Docket.</div>}
            {calls.data?.map((c) => (
              <div className="lg" key={c.id}>
                <span className="mono" style={{ color: 'var(--t3)' }}>{new Date(c.at).toLocaleTimeString('ru-RU')}</span>
                <span style={{ width: 20, height: 20, borderRadius: '50%', background: 'var(--acs)', color: 'var(--act)', display: 'grid', placeItems: 'center' }} title={c.actor}><Icon name="spark" size={11} sw={2.2} /></span>
                <span style={{ minWidth: 0, display: 'flex', flexDirection: 'column', gap: 1 }}>
                  <span className="mono" style={{ fontWeight: 500, fontSize: 12.5 }}>{c.tool}</span>
                  <span className="mono" style={{ fontSize: 11.5, color: 'var(--t3)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={c.args_summary}>{c.args_summary}</span>
                </span>
                <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span className="mono" style={{ fontSize: 11.5, color: 'var(--t3)' }}>{c.duration_ms} мс</span>
                  <span className={c.status === 'ok' ? 'okc' : 'erc'} title={c.error_code ?? undefined}>{c.status === 'ok' ? 'ok' : 'ошибка'}</span>
                </span>
              </div>
            ))}
            <div style={{ padding: '12px 18px', borderTop: '1px solid var(--ln)', display: 'flex', alignItems: 'center', fontSize: 12.5, color: 'var(--t3)' }}>
              <span className="grow">Хранится 7 дней</span>
              <ConfirmButton className="linkbtn" label="Очистить журнал" armed="Точно очистить?" disabled={clear.isPending || !calls.data?.length} onConfirm={() => clear.mutate()} />
            </div>
          </section>
        </aside>
      </div>
    </>
  )
}
