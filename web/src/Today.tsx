import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  type Activity, type Category, type Meta, type Project, type Task, type TodayData, addDays, api, fmtDay, fmtLongDay, fmtTime,
  plural, todayStr, useProjectsMeta,
} from './api'
import { Icon, ProjectTile } from './icons'
import { useFresh } from './live'
import { Link, hrefWith, navigate, useRoute } from './router'
import { AreaDot, ClaudeBadge, EmptyState, ErrorState, PriorityChip, SkeletonRows } from './ui'

const PRIO = { P0: 0, P1: 1, P2: 2 }
const byPriority = (a: Task, b: Task) => PRIO[a.priority] - PRIO[b.priority] || a.position - b.position

/** todo -> doing -> done -> todo. The status is the project's "todo" (else its first todo-category one), or the first of the category. */
function nextStatus(t: Task, meta: Meta | undefined) {
  const next: Category = t.status_category === 'todo' ? 'doing' : t.status_category === 'doing' ? 'done' : 'todo'
  const all = meta?.statuses ?? []
  return (next === 'todo' ? all.find((s) => s.key === 'todo') : undefined) ?? all.find((s) => s.category === next)
}

function TaskRow({ t, meta, late, onCycle }: { t: Task; meta?: Meta; late?: boolean; onCycle: (t: Task) => void }) {
  const fresh = useFresh(t.id)
  const done = t.status_category === 'done'
  return (
    <div className={'row' + (fresh ? ' fresh' : '')}>
      <button type="button" className={'sc ' + t.status_category} aria-label={`${t.code}: ${t.status_name}. Сменить статус`} onClick={() => onCycle(t)}>
        {done && <Icon name="check" size={11} sw={3.2} />}
      </button>
      <Link to={hrefWith('task', t.code)} className="rowmain">
        <span className="code mono">{t.code}</span>
        <span className={'title' + (done ? ' done' : '')}>{t.title}</span>
        {late && t.planned_for && <span className="mc" style={{ color: 'var(--rd)', fontWeight: 500 }}>{fmtDay(t.planned_for)}</span>}
        {t.created_by === 'claude' && <ClaudeBadge />}
        {!late && <AreaDot area={t.area} labels={meta?.labels ?? []} />}
        <PriorityChip p={t.priority} />
      </Link>
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
    case 'created': return `создал ${code}`
    case 'moved': return d.status ? `перевёл ${code} в «${d.status[1]}»` : `переместил ${code}`
    case 'updated':
      if (d.priority) return `изменил приоритет ${code}: ${d.priority[0]} → ${d.priority[1]}`
      return d.title ? `переименовал ${code}` : `обновил ${code}`
    case 'deleted': return `удалил ${code}`
    case 'restored': return `восстановил ${code}`
    case 'note_added': return `добавил заметку к ${code}`
    case 'carried_over': return `перенёс ${code} на ${fmtDay(String(d.planned_for?.[1] ?? ''))}`
    case 'project_created': return 'создал проект'
    case 'project_updated': return 'обновил проект'
    case 'project_deleted': return 'удалил проект в корзину'
    case 'project_restored': return 'восстановил проект'
    default: return a.action
  }
}

export function Today({ onNewTask }: { onNewTask: (o: { project?: Project; date: string }) => void }) {
  const { params } = useRoute()
  const dateParam = params.get('date') ?? undefined
  const qc = useQueryClient()
  const key = ['today', dateParam ?? 'now']
  const q = useQuery({ queryKey: key, queryFn: () => api.today(dateParam) })
  const mcp = useQuery({ queryKey: ['mcp-status'], queryFn: api.mcpStatus })
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
  const [confirm, setConfirm] = useState(false)
  useEffect(() => {
    if (!confirm) return
    const t = setTimeout(() => setConfirm(false), 4000)
    return () => clearTimeout(t)
  }, [confirm])

  const overdueRef = useRef<HTMLElement>(null)
  const focus = params.get('focus')
  useEffect(() => {
    if (focus === 'overdue' && data) overdueRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [focus, data])

  const date = data?.date ?? dateParam ?? todayStr()
  const isToday = date === todayStr()
  const total = data?.tasks.length ?? 0
  const done = data?.tasks.filter((t) => t.status_category === 'done').length ?? 0
  const doing = data?.tasks.filter((t) => t.status_category === 'doing').length ?? 0
  const rest = total - done - doing
  const open = total - done
  const go = (d: string) => navigate(d === todayStr() ? '/' : `/?date=${d}`)
  const overdueDates = [...new Set((data?.overdue ?? []).map((t) => t.planned_for!))]

  return (
    <>
      <header className="pagehead">
        <div className="stack">
          <h1>{isToday ? 'Сегодня' : fmtLongDay(date)}</h1>
          <span className="sub">
            {isToday ? fmtLongDay(date) : fmtDay(date)} · {total} {plural(total, 'задача', 'задачи', 'задач')} в {projects.length} {plural(projects.length, 'проекте', 'проектах', 'проектах')}
          </span>
        </div>
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <button type="button" className="btn sq" aria-label="Предыдущий день" onClick={() => go(addDays(date, -1))}><Icon name="chevL" size={16} sw={2} /></button>
          <button type="button" className="btn" onClick={() => go(todayStr())}>Сегодня</button>
          <button type="button" className="btn sq" aria-label="Следующий день" onClick={() => go(addDays(date, 1))}><Icon name="chevR" size={16} sw={2} /></button>
        </span>
        <button type="button" className={'btn' + (confirm ? ' warn' : '')} disabled={open === 0 || carry.isPending}
          onClick={() => (confirm ? (setConfirm(false), carry.mutate([[date, addDays(date, 1)]])) : setConfirm(true))}>
          <Icon name="arrowRight" size={15} sw={1.8} />
          {confirm ? `Перенести ${open} на ${fmtDay(addDays(date, 1)).toLowerCase()}?` : 'Незакрытые на завтра'}
        </button>
        <button type="button" className="btn pri" onClick={() => onNewTask({ date })}>
          <Icon name="plus" size={16} sw={2} />Новая задача<span className="kbd">C</span>
        </button>
      </header>

      <div className="content">
        <div className="col-main">
          {q.isPending && <section className="panel"><SkeletonRows n={5} /></section>}
          {q.isError && <section className="panel"><ErrorState message={q.error.message} onRetry={() => q.refetch()} /></section>}
          {data && (
            <>
              {total > 0 && (
                <section className="panel" style={{ padding: '16px 18px', display: 'flex', flexDirection: 'column', gap: 12 }}>
                  <div style={{ display: 'flex', alignItems: 'baseline', gap: 12, flexWrap: 'wrap' }}>
                    <span style={{ fontSize: 20, fontWeight: 700, letterSpacing: '-.01em' }}>{done} из {total} готово</span>
                    <span className="grow" />
                    <span className="mc"><span className="dot" style={{ background: 'var(--dn)' }} />Готово {done}</span>
                    <span className="mc"><span className="dot" style={{ background: 'var(--dg)' }} />В работе {doing}</span>
                    <span className="mc"><span className="dot" style={{ background: 'var(--td)' }} />К работе {rest}</span>
                  </div>
                  <div className="seg3" role="img" aria-label={`Готово ${done}, в работе ${doing}, к работе ${rest}`}>
                    {done > 0 && <span style={{ flex: done, background: 'var(--dn)' }} />}
                    {doing > 0 && <span style={{ flex: doing, background: 'var(--dg)' }} />}
                    {rest > 0 && <span style={{ flex: rest, background: 'var(--td)', opacity: 0.55 }} />}
                  </div>
                </section>
              )}

              {data.overdue.length > 0 && (
                <section className="panel" ref={overdueRef} style={{ overflow: 'hidden', borderColor: 'var(--rd)' }}>
                  <div className="panel-head">
                    <span className="dot" style={{ background: 'var(--rd)' }} />
                    <b style={{ fontWeight: 600 }}>Просрочено</b>
                    <span className="mono grow" style={{ fontSize: 12, color: 'var(--t3)' }}>{data.overdue.length}</span>
                    <button type="button" className="btn" disabled={carry.isPending} onClick={() => carry.mutate(overdueDates.map((d) => [d, todayStr()]))}>
                      Перенести всё на сегодня
                    </button>
                  </div>
                  {[...data.overdue].sort(byPriority).map((t) => <TaskRow key={t.id} t={t} late meta={meta[t.project_id]} onCycle={onCycle} />)}
                </section>
              )}

              {total === 0 && (
                <section className="panel">
                  <EmptyState icon="cal" title="На этот день задач нет"
                    hint="Добавьте задачу вручную или попросите Claude спланировать день через MCP."
                    action={<button type="button" className="btn pri" onClick={() => onNewTask({ date })}><Icon name="plus" size={16} sw={2} />Новая задача</button>} />
                </section>
              )}

              {projects.map((p) => {
                const ts = data.tasks.filter((t) => t.project_id === p.id).sort(byPriority)
                const d = ts.filter((t) => t.status_category === 'done').length
                return (
                  <section key={p.id} className="panel" style={{ overflow: 'hidden' }}>
                    <div className="panel-head">
                      <ProjectTile icon={p.icon} color={p.color} size={22} radius={7} />
                      <Link to={`/p/${p.slug}`} style={{ fontWeight: 600, textDecoration: 'none' }}>{p.name}</Link>
                      <span className="mono grow tabnum" style={{ fontSize: 12, color: 'var(--t3)' }}>{d} / {ts.length}</span>
                      <span className="bar" role="img" aria-label={`Готово ${d} из ${ts.length}`}><span style={{ width: `${(d / ts.length) * 100}%` }} /></span>
                    </div>
                    {ts.map((t) => <TaskRow key={t.id} t={t} meta={meta[p.id]} onCycle={onCycle} />)}
                    <button type="button" className="addline" onClick={() => onNewTask({ project: p, date })}>
                      <Icon name="plus" size={14} sw={2} />Добавить на {isToday ? 'сегодня' : fmtDay(date).toLowerCase()}
                    </button>
                  </section>
                )
              })}
            </>
          )}
        </div>

        <aside className="col-side">
          <Link to="/mcp" className={'panel mcpcard' + (mcp.data && !mcp.data.online ? ' off' : '')}>
            <span className="ic"><Icon name="plug" size={17} sw={1.9} /></span>
            <span style={{ display: 'flex', flexDirection: 'column', flexGrow: 1, lineHeight: 1.3 }}>
              <b style={{ fontWeight: 600 }}>{mcp.data && !mcp.data.online ? 'MCP не отвечает' : 'Claude подключён'}</b>
              <span style={{ fontSize: 12.5, color: 'var(--t3)' }}>
                {mcp.data ? `${mcp.data.median_ms} мс · ${mcp.data.calls_today} ${plural(mcp.data.calls_today, 'вызов', 'вызова', 'вызовов')} сегодня` : 'проверяю…'}
              </span>
            </span>
            <Icon name="chevR" size={16} sw={2} />
          </Link>
          <section className="panel" style={{ overflow: 'hidden' }}>
            <div className="panel-head"><b style={{ fontWeight: 600, flexGrow: 1 }}>Активность</b><span className="cap">последняя</span></div>
            {data && data.activity.length === 0 && <div className="feed"><span style={{ color: 'var(--t3)', fontSize: 13 }}>Пока ничего не происходило</span></div>}
            {data?.activity.map((a) => (
              <div className="feed" key={a.id}>
                <span className={'ic ' + (a.actor === 'claude' ? 'ai' : 'me')}><Icon name={a.actor === 'claude' ? 'spark' : 'user'} size={13} sw={2} /></span>
                <span className="tx">
                  <span><b style={{ fontWeight: 600 }}>{a.actor === 'claude' ? 'claude' : 'Denis'}</b> {actText(a)}</span>
                  <span className="mono" style={{ fontSize: 11.5, color: 'var(--t3)' }}>{fmtTime(a.at)}</span>
                </span>
              </div>
            ))}
          </section>
        </aside>
      </div>
    </>
  )
}
