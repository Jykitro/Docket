import { useEffect, useRef } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  type Activity, type Category, type Meta, type Project, type Task, type TodayData, addDays, agentName, api, fmtDay, fmtLongDay, fmtStamp, fmtTime, isoDay,
  mcpHealthLabel, mcpLastCallLabel, todayStr, isAgent, useProjects, useProjectsMeta, useWho,
} from './api'
import { Icon, ProjectTile } from './icons'
import { locale, plural, tr } from './i18n'
import { useFresh } from './live'
import { Link, hrefWith, navigate, scrollToSection, useRoute } from './router'
import { AreaDot, AgentBadge, ConfirmButton, ErrorState, Menu, PriorityChip, SkeletonRows, btn, btnPri, btnSq, cap, kbdPri, mc } from './ui'

const panel = 'bg-sf border border-ln rounded-xl'
const panelHead = 'flex flex-wrap items-center gap-2.5 px-4 py-3'
const dot = 'inline-block w-2 h-2 rounded-full flex-[0_0_8px] '
const feed = 'flex gap-3 px-4 py-3 border-t border-ln'
const content = 'flex flex-wrap items-start gap-4 px-4 pb-8 pt-3 sm:gap-6 sm:px-8 sm:pt-6'
const freshRing = ' shadow-[inset_0_0_0_2px_var(--ac)]'
const SC: Record<Category, string> = {
  todo: 'border-t3 bg-transparent hover:border-ac',
  doing: 'border-dg bg-[linear-gradient(90deg,var(--dg)_50%,transparent_50%)]',
  done: 'border-dn bg-dn',
}

const PRIO = { P0: 0, P1: 1, P2: 2 }
const byPriority = (a: Task, b: Task) => PRIO[a.priority] - PRIO[b.priority] || a.position - b.position

/** todo -> doing -> done -> todo. The status is the project's "todo" (else its first todo-category one), or the first of the category. */
function nextStatus(t: Task, meta: Meta | undefined) {
  const next: Category = t.status_category === 'todo' ? 'doing' : t.status_category === 'doing' ? 'done' : 'todo'
  const all = meta?.statuses ?? []
  return (next === 'todo' ? all.find((s) => s.key === 'todo') : undefined) ?? all.find((s) => s.category === next)
}

function StatusButton({ t, onCycle }: { t: Task; onCycle: (t: Task) => void }) {
  return (
    <button type="button" className="-mx-2 grid size-9 shrink-0 place-items-center rounded-md hover:bg-sk" aria-label={`${t.code}: ${t.status_name}. ${tr('Сменить статус')}`} onClick={() => onCycle(t)}>
      <span className={'grid size-[18px] place-items-center rounded-full border-[1.5px] text-white ' + SC[t.status_category]}>{t.status_category === 'done' && <Icon name="check" size={11} sw={3.2} />}</span>
    </button>
  )
}

function TaskRow({ t, meta, onCycle }: { t: Task; meta?: Meta; onCycle: (t: Task) => void }) {
  const fresh = useFresh(t.id)
  const done = t.status_category === 'done'
  return (
    <div className={'flex items-center gap-3 min-h-[46px] px-4 py-1.5 border-t border-ln no-underline text-inherit hover:bg-sf2' + (fresh ? freshRing : '')}>
      <StatusButton t={t} onCycle={onCycle} />
      <Link to={hrefWith('task', t.code)} className="flex flex-wrap items-center gap-x-3 gap-y-1 flex-1 min-w-0 self-stretch no-underline text-inherit">
        <span className="font-mono text-[11.5px] text-t3 w-[58px] flex-[0_0_58px]">{t.code}</span>
        <span className={'flex-[1_1_200px] min-w-0 wrap-anywhere font-medium' + (done ? ' text-t3 line-through' : '')}>{t.title}</span>
        {isAgent(t.created_by) && <AgentBadge actor={t.created_by} />}
        <AreaDot area={t.area} labels={meta?.labels ?? []} />
        <PriorityChip p={t.priority} />
      </Link>
    </div>
  )
}

// Overdue table: a grid row from md up; below md the same cells wrap into a two-line card, so there is no horizontal scroll.
const lateCols = 'md:grid md:grid-cols-[28px_72px_minmax(0,1fr)_minmax(0,150px)_minmax(0,120px)_28px_84px] md:gap-x-3'
const dayMs = 86_400_000

function OverdueRow({ t, project, date, onCycle }: { t: Task; project?: Project; date: string; onCycle: (t: Task) => void }) {
  const fresh = useFresh(t.id)
  const who = useWho()
  const days = Math.round((Date.parse(date + 'T12:00:00') - Date.parse(t.planned_for + 'T12:00:00')) / dayMs)
  return (
    <div className={'flex flex-wrap items-center gap-x-3 gap-y-1 min-h-11 px-4 py-1.5 border-t border-ln hover:bg-sf2 ' + lateCols + (fresh ? freshRing : '')}>
      <StatusButton t={t} onCycle={onCycle} />
      <span className="font-mono text-[11.5px] text-t3">{t.code}</span>
      <Link to={hrefWith('task', t.code)} className="flex-[1_1_180px] min-w-0 line-clamp-2 wrap-anywhere font-medium no-underline text-inherit hover:underline">{t.title}</Link>
      <span className="flex min-w-0 items-center gap-2 text-[13px] text-t2 max-md:basis-full max-md:pl-[42px] md:contents">
        <span className="flex min-w-0 items-center gap-2">
          {project && <ProjectTile project={project} size={16} radius={5} />}
          <span className="truncate">{project?.name}</span>
        </span>
        <span className="flex min-w-0">{isAgent(t.created_by) ? <AgentBadge actor={t.created_by} /> : <span className="truncate">{who(t.created_by)}</span>}</span>
        <PriorityChip p={t.priority} />
        <span className="ml-auto text-[12.5px] font-medium text-rd tabular-nums md:text-right" title={tr('Срок: {day}', { day: fmtLongDay(t.planned_for!) })}>
          {plural(days, '{n} день|{n} дня|{n} дней')}
        </span>
      </span>
    </div>
  )
}

function actText(a: Activity): string {
  const code = a.task_code
  let d: Record<string, [unknown, unknown]> = {}
  try {
    d = JSON.parse(a.diff_json)
  } catch {
    /* an unreadable diff just falls back to the generic text */
  }
  switch (a.action) {
    case 'created': return tr('Создана задача {code}', { code })
    case 'moved': return d.status ? tr('{code} → {status}', { code, status: String(d.status[1]) }) : tr('Задача {code} перемещена', { code })
    case 'updated':
      if (d.priority) return tr('Приоритет {code} · {from} → {to}', { code, from: String(d.priority[0]), to: String(d.priority[1]) })
      return d.title ? tr('Задача {code} переименована', { code }) : tr('Задача {code} обновлена', { code })
    case 'deleted': return tr('Задача {code} удалена', { code })
    case 'restored': return tr('Задача {code} восстановлена', { code })
    case 'html_published': return tr('Опубликован HTML-документ')
    case 'html_deleted': return tr('Удалён HTML-документ')
    case 'note_added': return tr('Добавлена заметка к {code}', { code })
    case 'carried_over': return tr('Задача {code} перенесена на {day}', { code, day: fmtDay(String(d.planned_for?.[1] ?? '')) })
    case 'project_created': return tr('Проект создан')
    case 'project_updated': return tr('Проект обновлён')
    case 'project_deleted': return tr('Проект перемещён в корзину')
    case 'project_restored': return tr('Проект восстановлен')
    default: return a.action
  }
}

/** Consecutive events of one task by one actor collapse into one row; rows are grouped by day (newest first, as the API sends them). */
function groupActivity(list: Activity[]) {
  const days: { day: string; rows: Activity[][] }[] = []
  for (const a of list) {
    const day = isoDay(new Date(a.at))
    let d = days[days.length - 1]
    if (d?.day !== day) days.push(d = { day, rows: [] })
    const r = d.rows[d.rows.length - 1]
    if (r && a.task_code && r[0].task_code === a.task_code && r[0].actor === a.actor) r.push(a)
    else d.rows.push([a])
  }
  return days
}

/** The group's main action (the latest that is not a note) plus the number of notes. */
function groupText(r: Activity[]) {
  const main = r.find((a) => a.action !== 'note_added') ?? r[0]
  const notes = r.filter((a) => a.action === 'note_added').length
  const n = plural(notes, '{n} заметка|{n} заметки|{n} заметок')
  if (main.action === 'note_added') return notes > 1 ? `${main.task_code} · ${n}` : actText(main)
  return notes ? `${actText(main)} · ${n}` : actText(main)
}

function dayHead(day: string) {
  const s = new Date(day + 'T12:00:00').toLocaleDateString(locale(), { day: 'numeric', month: 'long' })
  const today = todayStr()
  return day === today ? tr('сегодня, {day}', { day: s }) : day === addDays(today, -1) ? tr('вчера, {day}', { day: s }) : s
}

export function Today({ onNewTask }: { onNewTask: (o: { project?: Project; date: string }) => void }) {
  const { params } = useRoute()
  const who = useWho()
  const dateParam = params.get('date') ?? undefined
  const qc = useQueryClient()
  const key = ['today', dateParam ?? 'now']
  const q = useQuery({ queryKey: key, queryFn: () => api.today(dateParam) })
  const mcp = useQuery({ queryKey: ['mcp-status'], queryFn: api.mcpStatus })
  const calls = useQuery({ queryKey: ['mcp-calls', 'all'], queryFn: () => api.mcpCalls('all') })
  const allProjects = useProjects().data
  const data = q.data
  const projects = (data?.projects ?? []).filter((p) => data!.tasks.some((t) => t.project_id === p.id))
  const meta = useProjectsMeta((data?.projects ?? []).map((p) => p.id))

  const cycle = useMutation({
    mutationFn: ({ t, status }: { t: Task; status: string }) => api.moveTask(t.code, { status }),
    onMutate: async ({ t, status }) => {
      await qc.cancelQueries({ queryKey: key })
      const prev = qc.getQueryData<TodayData>(key)
      const st = meta[t.project_id]?.statuses.find((s) => s.key === status)
      const patch = (x: Task) => (x.id === t.id && st ? { ...x, status, status_name: st.name, status_category: st.category } : x)
      if (prev) qc.setQueryData<TodayData>(key, { ...prev, tasks: prev.tasks.map(patch), overdue: prev.overdue.map(patch) })
      return { prev }
    },
    onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(key, ctx.prev),
    onSettled: () => qc.invalidateQueries(),
  })
  const onCycle = (t: Task) => {
    const st = nextStatus(t, meta[t.project_id])
    if (st) cycle.mutate({ t, status: st.key })
  }

  const carry = useMutation({
    mutationFn: async (pairs: [string, string][]) => { for (const [from, to] of pairs) await api.carryOver(from, to) },
    onSettled: () => qc.invalidateQueries(),
  })
  const overdueRef = useRef<HTMLElement>(null)
  const focus = params.get('focus')
  useEffect(() => {
    if (focus === 'overdue' && data) scrollToSection(overdueRef.current)
  }, [focus, data])

  const date = data?.date ?? dateParam ?? todayStr()
  const isToday = date === todayStr()
  const total = data?.tasks.length ?? 0
  const done = data?.tasks.filter((t) => t.status_category === 'done').length ?? 0
  const doing = data?.tasks.filter((t) => t.status_category === 'doing').length ?? 0
  const rest = total - done - doing
  const open = total - done
  const go = (d: string) => navigate(d === todayStr() ? '/' : `/?date=${d}`)
  const overdueDates = [...new Set((data?.overdue ?? []).map((t) => t.planned_for!))].sort()
  const moveOverdue = () => carry.mutate(overdueDates.map((d) => [d, todayStr()]))
  const lateSpan = overdueDates.length > 1 ? `${fmtDay(overdueDates[0])} – ${fmtDay(overdueDates[overdueDates.length - 1])}` : overdueDates[0] ? fmtDay(overdueDates[0]) : ''
  // shortcut: agents come from the last 50 calls of the MCP log, so an agent quiet for longer is not listed; a per-actor API would fix that.
  const lastCall = new Map<string, string>()
  for (const c of calls.data ?? []) if (c.at > (lastCall.get(c.actor) ?? '')) lastCall.set(c.actor, c.at)
  const agents = [...lastCall].sort((a, b) => b[1].localeCompare(a[1]))

  const carryButton = (close?: () => void) => (
    <ConfirmButton key={date} icon={<Icon name="arrowRight" size={15} />} disabled={open === 0 || carry.isPending}
      label={tr('Незакрытые на завтра')} armed={tr('Перенести {n} на {day}?', { n: open, day: fmtDay(addDays(date, 1)).toLowerCase() })}
      onConfirm={() => { close?.(); carry.mutate([[date, addDays(date, 1)]]) }} />
  )

  return (
    <>
      <header className="flex flex-wrap items-center gap-2 px-4 pt-4 pb-2 sm:gap-3.5 sm:px-8 sm:pt-6 sm:pb-4">
        <div className="flex flex-col gap-0.5 basis-full min-w-0 sm:flex-[1_1_260px]">
          <h1 className="text-2xl font-bold tracking-tight">{isToday ? tr('Сегодня') : fmtLongDay(date)}</h1>
          <span className="text-sm text-t3">
            {isToday ? fmtLongDay(date) : fmtDay(date)} · {tr('{tasks} в {projects}', {
              tasks: plural(total, '{n} задача|{n} задачи|{n} задач'),
              projects: plural(projects.length, '{n} проекте|{n} проектах|{n} проектах'),
            })}
          </span>
        </div>
        <span className="inline-flex items-center gap-1 sm:gap-1.5">
          <button type="button" className={btnSq + ' max-sm:w-8'} aria-label={tr('Предыдущий день')} onClick={() => go(addDays(date, -1))}><Icon name="chevL" size={16} sw={2} /></button>
          <button type="button" className={btn + ' max-sm:px-2'} onClick={() => go(todayStr())}>{tr('Сегодня')}</button>
          <button type="button" className={btnSq + ' max-sm:w-8'} aria-label={tr('Следующий день')} onClick={() => go(addDays(date, 1))}><Icon name="chevR" size={16} sw={2} /></button>
        </span>
        <div className="hidden sm:block">{carryButton()}</div>
        <button type="button" className={btnPri + ' max-sm:ml-auto max-sm:gap-1 max-sm:px-2'} onClick={() => onNewTask({ date })}>
          <Icon name="plus" size={16} sw={2} />{tr('Новая задача')}<span className={kbdPri + ' max-sm:hidden'}>C</span>
        </button>
        <div className="sm:hidden"><Menu right className={btnSq + ' max-sm:w-8'} ariaLabel={tr('Действия дня')} label={<Icon name="more" />}>{carryButton}</Menu></div>
      </header>

      <div className={content}>
        <div className="flex-[999_1_520px] min-w-0 flex flex-col gap-[18px]">
          {q.isPending && <section className={panel}><SkeletonRows n={5} /></section>}
          {q.isError && <section className={panel}><ErrorState error={q.error} onRetry={() => q.refetch()} /></section>}
          {data && (
            <>
              {total > 0 && (
                <section className={panel + ' py-4 px-[18px] flex flex-col gap-3'}>
                  <div className="flex items-baseline gap-3 flex-wrap">
                    <span className="text-[20px] font-bold tracking-[-.01em]">{tr('{done} из {total} готово', { done, total })}</span>
                    <span className="grow" />
                    <span className={mc}><span className={dot + 'bg-dn'} />{tr('Готово {n}', { n: done })}</span>
                    <span className={mc}><span className={dot + 'bg-dg'} />{tr('В работе {n}', { n: doing })}</span>
                    <span className={mc}><span className={dot + 'bg-td'} />{tr('К работе {n}', { n: rest })}</span>
                  </div>
                  <div className="flex gap-[3px] h-2" role="img" aria-label={tr('Готово {done}, в работе {doing}, к работе {rest}', { done, doing, rest })}>
                    {done > 0 && <span className="rounded-[4px] bg-dn" style={{ flex: done }} />}
                    {doing > 0 && <span className="rounded-[4px] bg-dg" style={{ flex: doing }} />}
                    {rest > 0 && <span className="rounded-[4px] bg-td opacity-[.55]" style={{ flex: rest }} />}
                  </div>
                </section>
              )}

              {data.overdue.length > 0 && (
                <section className="scroll-mt-20 bg-sf border border-ln rounded-xl overflow-hidden" ref={overdueRef} aria-labelledby="overdue-h">
                  <div className={panelHead}>
                    <div className="flex min-w-0 grow flex-col">
                      <h2 id="overdue-h" className="flex items-center gap-2 font-semibold">
                        {tr('Просроченные задачи')}<span className="rounded-full bg-rds px-1.5 text-xs font-semibold text-rd tabular-nums">{data.overdue.length}</span>
                      </h2>
                      <p className="text-[13px] text-t3">{tr('Срок прошёл {when}', { when: lateSpan })}</p>
                    </div>
                    <button type="button" className={btn} disabled={carry.isPending} onClick={moveOverdue}>{tr('Перенести всё на сегодня')}</button>
                  </div>
                  <div className={'hidden px-4 py-2 border-t border-ln bg-sf2 text-xs font-medium text-t3 ' + lateCols} aria-hidden="true">
                    <span /><span>{tr('Ключ')}</span><span>{tr('Задача')}</span><span>{tr('Проект')}</span><span>{tr('Автор')}</span>
                    <span title={tr('Приоритет')}>{tr('Пр.')}</span><span className="text-right">{tr('Просрочено')}</span>
                  </div>
                  {[...data.overdue].sort(byPriority).map((t) => (
                    <OverdueRow key={t.id} t={t} date={date} project={allProjects?.find((p) => p.id === t.project_id)} onCycle={onCycle} />
                  ))}
                </section>
              )}

              {total === 0 && (
                <section className={panel + ' flex flex-wrap items-center gap-3 px-4 py-3'} aria-labelledby="plan-h">
                  <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-sk text-t2"><Icon name="cal" size={17} /></span>
                  <div className="flex min-w-0 flex-[1_1_260px] flex-col gap-0.5">
                    <h2 id="plan-h" className="font-semibold">{isToday ? tr('План на сегодня пуст') : tr('На этот день задач нет')}</h2>
                    <p className="text-[13px] text-t3">
                      {tr('Попросите агента:')} <code className="rounded bg-sk px-1 py-0.5 font-mono text-[12px] text-t2">{tr('спланируй мой день в Plandillo')}</code>
                    </p>
                  </div>
                  {isToday && data.overdue.length > 0 && (
                    <button type="button" className={btn} disabled={carry.isPending} onClick={moveOverdue}>
                      {plural(data.overdue.length, 'Перенести {n} просроченную|Перенести {n} просроченные|Перенести {n} просроченных')}
                    </button>
                  )}
                  <button type="button" className={btnPri} onClick={() => onNewTask({ date })}><Icon name="plus" size={16} sw={2} />{tr('Добавить задачу')}</button>
                </section>
              )}

              {projects.map((p) => {
                const ts = data.tasks.filter((t) => t.project_id === p.id).sort(byPriority)
                const d = ts.filter((t) => t.status_category === 'done').length
                return (
                  <section key={p.id} className={panel + ' overflow-hidden'}>
                    <div className={panelHead}>
                      <ProjectTile project={p} size={22} radius={7} />
                      <Link to={`/p/${p.slug}`} className="font-semibold no-underline">{p.name}</Link>
                      <span className="font-mono grow tabular-nums text-[12px] text-t3">{d} / {ts.length}</span>
                      <span className="w-24 h-1.5 rounded-[3px] bg-sk overflow-hidden block" role="img" aria-label={tr('Готово {done} из {total}', { done: d, total: ts.length })}><span className="block h-full bg-dn" style={{ width: `${(d / ts.length) * 100}%` }} /></span>
                    </div>
                    {ts.map((t) => <TaskRow key={t.id} t={t} meta={meta[p.id]} onCycle={onCycle} />)}
                    <button type="button" className="flex items-center gap-2 min-h-10 px-4 py-1 border-t border-ln bg-transparent text-t3 text-[13px] w-full text-left hover:bg-sf2 hover:text-tx" onClick={() => onNewTask({ project: p, date })}>
                      <Icon name="plus" size={14} sw={2} />{tr('Добавить на {day}', { day: isToday ? tr('сегодня') : fmtDay(date).toLowerCase() })}
                    </button>
                  </section>
                )
              })}
            </>
          )}
        </div>

        <aside className="flex-[1_1_300px] max-w-[380px] min-w-0 flex flex-col gap-[18px] max-[640px]:max-w-none">
          <section className={panel + ' overflow-hidden'} aria-labelledby="agents-h">
            <div className={panelHead}>
              <h2 id="agents-h" className="font-semibold grow">{tr('Агенты')}</h2>
              <Link to="/integrations" className="text-[12.5px] text-act no-underline hover:underline">{tr('Интеграции')}</Link>
            </div>
            {agents.length === 0 && (
              <div className={feed + ' flex-col gap-0.5 text-[13px]'}>
                <span>{mcpHealthLabel(mcp.data, mcp.isError)}</span>
                {mcp.data && <span className="text-xs text-t3">{mcpLastCallLabel(mcp.data)}</span>}
              </div>
            )}
            {agents.map(([actor, at]) => (
              <div className={feed + ' items-center'} key={actor}>
                <span className="grid size-[26px] shrink-0 place-items-center rounded-full bg-acs text-act"><Icon name="spark" size={13} sw={2} /></span>
                <span className="flex min-w-0 grow flex-col text-[13px] leading-[1.4]">
                  <span className="truncate font-medium">{agentName(actor)}</span>
                  <span className="text-xs text-t3">{tr('Последний вызов · {when}', { when: fmtStamp(at) })}</span>
                </span>
              </div>
            ))}
            {!!mcp.data?.calls_today && (
              <div className="border-t border-ln px-4 py-2 text-xs text-t3">{tr('{calls} сегодня · медиана {ms} мс', { calls: mcp.data.calls_today, ms: mcp.data.median_ms })}</div>
            )}
          </section>

          <section className={panel + ' overflow-hidden'} aria-labelledby="act-h">
            <div className={panelHead}><h2 id="act-h" className="font-semibold grow">{tr('Активность')}</h2></div>
            {data && data.activity.length === 0 && <div className={feed}><span className="text-t3 text-[13px]">{tr('Пока ничего не происходило')}</span></div>}
            {data && groupActivity(data.activity).map((d) => (
              <div key={d.day}>
                <div className={cap + ' border-t border-ln bg-sf2 px-4 py-1.5'}>{dayHead(d.day)}</div>
                <ol>
                  {d.rows.map((r) => {
                    const a = r[0]
                    const from = fmtTime(r[r.length - 1].at)
                    const to = fmtTime(a.at)
                    return (
                      <li className={feed} key={a.id}>
                        <span className={'size-[26px] rounded-full flex-[0_0_26px] grid place-items-center ' + (isAgent(a.actor) ? 'bg-acs text-act' : 'bg-sk text-t2')}><Icon name={isAgent(a.actor) ? 'spark' : 'user'} size={13} sw={2} /></span>
                        <span className="flex-[1_1_0] min-w-0 flex flex-col gap-0.5 text-[13px] leading-[1.4]">
                          <span>{groupText(r)}</span>
                          <span className="text-xs text-t3">{who(a.actor)} · <time dateTime={a.at} className="tabular-nums">{from === to ? to : `${from}–${to}`}</time></span>
                        </span>
                      </li>
                    )
                  })}
                </ol>
              </div>
            ))}
          </section>
        </aside>
      </div>
    </>
  )
}
