import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  type Activity, type Category, type Meta, type Project, type Task, type TodayData, addDays, api, fmtDay, fmtLongDay, fmtTime,
  plural, todayStr, useProjectsMeta, useWho,
} from './api'
import { Icon, ProjectTile } from './icons'
import { useFresh } from './live'
import { Link, hrefWith, navigate, useRoute } from './router'
import { AreaDot, ClaudeBadge, EmptyState, ErrorState, PriorityChip, SkeletonRows, btn, btnPri, btnSq, btnWarn, cap, kbdPri, mc } from './ui'

const panel = 'bg-sf border border-ln rounded-xl'
const panelHead = 'flex flex-wrap items-center gap-2.5 px-4 py-3'
const dot = 'inline-block w-2 h-2 rounded-full flex-[0_0_8px] '
const feed = 'flex gap-3 px-4 py-3 border-t border-ln'
const content = 'px-8 pt-6 pb-8 flex gap-6 flex-wrap items-start max-[640px]:px-4'
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

function TaskRow({ t, meta, late, onCycle }: { t: Task; meta?: Meta; late?: boolean; onCycle: (t: Task) => void }) {
  const fresh = useFresh(t.id)
  const done = t.status_category === 'done'
  return (
    <div className={'flex items-center gap-3 min-h-[46px] px-4 py-1.5 border-t border-ln no-underline text-inherit hover:bg-sf2' + (fresh ? ' shadow-[inset_0_0_0_2px_var(--ac)]' : '')}>
      <button type="button" className={'size-[18px] rounded-full flex-[0_0_18px] grid place-items-center p-0 border-[1.5px] text-white ' + SC[t.status_category]} aria-label={`${t.code}: ${t.status_name}. Сменить статус`} onClick={() => onCycle(t)}>
        {done && <Icon name="check" size={11} sw={3.2} />}
      </button>
      <Link to={hrefWith('task', t.code)} className="flex flex-wrap items-center gap-x-3 gap-y-1 flex-1 min-w-0 self-stretch no-underline text-inherit">
        <span className="font-mono text-[11.5px] text-t3 w-[58px] flex-[0_0_58px]">{t.code}</span>
        <span className={'flex-[1_1_200px] min-w-0 font-medium' + (done ? ' text-t3 line-through' : '')}>{t.title}</span>
        {late && t.planned_for && <span className="inline-flex items-center gap-[5px] text-[12px] text-rd font-medium">{fmtDay(t.planned_for)}</span>}
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
  const who = useWho()
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
      <header className="px-8 pt-6 pb-4 flex items-center gap-3.5 flex-wrap max-[640px]:px-4">
        <div className="flex flex-col gap-0.5 flex-[1_1_260px] min-w-0">
          <h1 className="text-2xl font-bold tracking-tight">{isToday ? 'Сегодня' : fmtLongDay(date)}</h1>
          <span className="text-sm text-t3">
            {isToday ? fmtLongDay(date) : fmtDay(date)} · {total} {plural(total, 'задача', 'задачи', 'задач')} в {projects.length} {plural(projects.length, 'проекте', 'проектах', 'проектах')}
          </span>
        </div>
        <span className="inline-flex items-center gap-1.5">
          <button type="button" className={btnSq} aria-label="Предыдущий день" onClick={() => go(addDays(date, -1))}><Icon name="chevL" size={16} sw={2} /></button>
          <button type="button" className={btn} onClick={() => go(todayStr())}>Сегодня</button>
          <button type="button" className={btnSq} aria-label="Следующий день" onClick={() => go(addDays(date, 1))}><Icon name="chevR" size={16} sw={2} /></button>
        </span>
        <button type="button" className={confirm ? btnWarn : btn} disabled={open === 0 || carry.isPending}
          onClick={() => (confirm ? (setConfirm(false), carry.mutate([[date, addDays(date, 1)]])) : setConfirm(true))}>
          <Icon name="arrowRight" size={15} sw={1.8} />
          {confirm ? `Перенести ${open} на ${fmtDay(addDays(date, 1)).toLowerCase()}?` : 'Незакрытые на завтра'}
        </button>
        <button type="button" className={btnPri} onClick={() => onNewTask({ date })}>
          <Icon name="plus" size={16} sw={2} />Новая задача<span className={kbdPri}>C</span>
        </button>
      </header>

      <div className={content}>
        <div className="flex-[999_1_520px] min-w-0 flex flex-col gap-[18px]">
          {q.isPending && <section className={panel}><SkeletonRows n={5} /></section>}
          {q.isError && <section className={panel}><ErrorState message={q.error.message} onRetry={() => q.refetch()} /></section>}
          {data && (
            <>
              {total > 0 && (
                <section className={panel + ' py-4 px-[18px] flex flex-col gap-3'}>
                  <div className="flex items-baseline gap-3 flex-wrap">
                    <span className="text-[20px] font-bold tracking-[-.01em]">{done} из {total} готово</span>
                    <span className="grow" />
                    <span className={mc}><span className={dot + 'bg-dn'} />Готово {done}</span>
                    <span className={mc}><span className={dot + 'bg-dg'} />В работе {doing}</span>
                    <span className={mc}><span className={dot + 'bg-td'} />К работе {rest}</span>
                  </div>
                  <div className="flex gap-[3px] h-2" role="img" aria-label={`Готово ${done}, в работе ${doing}, к работе ${rest}`}>
                    {done > 0 && <span className="rounded-[4px] bg-dn" style={{ flex: done }} />}
                    {doing > 0 && <span className="rounded-[4px] bg-dg" style={{ flex: doing }} />}
                    {rest > 0 && <span className="rounded-[4px] bg-td opacity-[.55]" style={{ flex: rest }} />}
                  </div>
                </section>
              )}

              {data.overdue.length > 0 && (
                <section className="bg-sf border border-rd rounded-xl overflow-hidden" ref={overdueRef}>
                  <div className={panelHead}>
                    <span className={dot + 'bg-rd'} />
                    <b className="font-semibold">Просрочено</b>
                    <span className="font-mono grow text-[12px] text-t3">{data.overdue.length}</span>
                    <button type="button" className={btn} disabled={carry.isPending} onClick={() => carry.mutate(overdueDates.map((d) => [d, todayStr()]))}>
                      Перенести всё на сегодня
                    </button>
                  </div>
                  {[...data.overdue].sort(byPriority).map((t) => <TaskRow key={t.id} t={t} late meta={meta[t.project_id]} onCycle={onCycle} />)}
                </section>
              )}

              {total === 0 && (
                <section className={panel}>
                  <EmptyState icon="cal" title="На этот день задач нет"
                    hint="Добавьте задачу вручную или попросите Claude спланировать день через MCP."
                    action={<button type="button" className={btnPri} onClick={() => onNewTask({ date })}><Icon name="plus" size={16} sw={2} />Новая задача</button>} />
                </section>
              )}

              {projects.map((p) => {
                const ts = data.tasks.filter((t) => t.project_id === p.id).sort(byPriority)
                const d = ts.filter((t) => t.status_category === 'done').length
                return (
                  <section key={p.id} className={panel + ' overflow-hidden'}>
                    <div className={panelHead}>
                      <ProjectTile icon={p.icon} color={p.color} size={22} radius={7} />
                      <Link to={`/p/${p.slug}`} className="font-semibold no-underline">{p.name}</Link>
                      <span className="font-mono grow tabular-nums text-[12px] text-t3">{d} / {ts.length}</span>
                      <span className="w-24 h-1.5 rounded-[3px] bg-sk overflow-hidden block" role="img" aria-label={`Готово ${d} из ${ts.length}`}><span className="block h-full bg-dn" style={{ width: `${(d / ts.length) * 100}%` }} /></span>
                    </div>
                    {ts.map((t) => <TaskRow key={t.id} t={t} meta={meta[p.id]} onCycle={onCycle} />)}
                    <button type="button" className="flex items-center gap-2 min-h-10 px-4 py-1 border-t border-ln bg-transparent text-t3 text-[13px] w-full text-left hover:bg-sf2 hover:text-tx" onClick={() => onNewTask({ project: p, date })}>
                      <Icon name="plus" size={14} sw={2} />Добавить на {isToday ? 'сегодня' : fmtDay(date).toLowerCase()}
                    </button>
                  </section>
                )
              })}
            </>
          )}
        </div>

        <aside className="flex-[1_1_300px] max-w-[380px] min-w-0 flex flex-col gap-[18px] max-[640px]:max-w-none">
          <Link to="/mcp" className={panel + ' flex items-center gap-3 px-4 py-3.5 no-underline text-inherit'}>
            <span className={'size-[34px] rounded-[9px] grid place-items-center ' + (mcp.data && !mcp.data.online ? 'bg-rds text-rd' : 'bg-dns text-dnt')}><Icon name="plug" size={17} sw={1.9} /></span>
            <span className="flex flex-col grow leading-[1.3]">
              <b className="font-semibold">{mcp.data && !mcp.data.online ? 'MCP не отвечает' : 'Claude подключён'}</b>
              <span className="text-[12.5px] text-t3">
                {mcp.data ? `${mcp.data.median_ms} мс · ${mcp.data.calls_today} ${plural(mcp.data.calls_today, 'вызов', 'вызова', 'вызовов')} сегодня` : 'проверяю…'}
              </span>
            </span>
            <Icon name="chevR" size={16} sw={2} />
          </Link>
          <section className={panel + ' overflow-hidden'}>
            <div className={panelHead}><b className="font-semibold grow">Активность</b><span className={cap}>последняя</span></div>
            {data && data.activity.length === 0 && <div className={feed}><span className="text-t3 text-[13px]">Пока ничего не происходило</span></div>}
            {data?.activity.map((a) => (
              <div className={feed} key={a.id}>
                <span className={'size-[26px] rounded-full flex-[0_0_26px] grid place-items-center ' + (a.actor === 'claude' ? 'bg-acs text-act' : 'bg-sk text-t2')}><Icon name={a.actor === 'claude' ? 'spark' : 'user'} size={13} sw={2} /></span>
                <span className="flex-[1_1_0] min-w-0 flex flex-col gap-0.5 text-[13px] leading-[1.4]">
                  <span><b className="font-semibold">{who(a.actor)}</b> {actText(a)}</span>
                  <span className="font-mono text-[11.5px] text-t3">{fmtTime(a.at)}</span>
                </span>
              </div>
            ))}
          </section>
        </aside>
      </div>
    </>
  )
}
