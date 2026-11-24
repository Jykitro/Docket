import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type AttentionItem, addDays, api, todayStr, useAttention } from './api'
import { Link, hrefWith } from './router'
import { tr } from './i18n'
import { EmptyState, ErrorState, PriorityChip, SkeletonRows, btn } from './ui'

const groups = [
  ['blocker', 'Блокирует работу'], ['decision', 'Нужно решение'],
  ['problem', 'Проблемы'], ['unverified', 'Требует проверки'],
] as const

function IssueRow({ task }: { task: AttentionItem }) {
  const qc = useQueryClient()
  const [form, setForm] = useState<'snooze' | 'close' | null>(null)
  const [value, setValue] = useState('')
  const change = useMutation({
    mutationFn: (action: 'take' | 'snooze' | 'close') => api.triage(task.id, action, value),
    onSuccess: () => { setForm(null); return qc.invalidateQueries() },
  })
  function edit(action: 'snooze' | 'close') {
    change.reset()
    setForm(action)
    setValue(action === 'snooze' ? addDays(todayStr(), 1) : '')
  }
  return <li className="border-t border-ln px-4 py-3">
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
      <div className="min-w-0 flex-[1_1_260px]">
        <div className="flex flex-wrap items-center gap-2 text-xs text-t3">
          <span>{task.project}</span><span className="font-mono">{task.code}</span><PriorityChip p={task.priority} />
          <span>{task.status_name}</span>
          {!!task.report_count && <span>{tr('Наблюдений: {n}', { n: task.report_count })}</span>}
        </div>
        <Link className="mt-1 block wrap-anywhere text-sm font-semibold hover:underline" to={hrefWith('task', task.code)}>{task.title}</Link>
      </div>
      <div className="flex flex-wrap gap-2">
        {task.status_category !== 'doing' && <button className={btn} disabled={change.isPending} onClick={() => change.mutate('take')}>{tr('Взять в работу')}</button>}
        <button className={btn} disabled={change.isPending} onClick={() => edit('snooze')}>{tr('Отложить')}</button>
        <button className={btn} disabled={change.isPending} onClick={() => edit('close')}>{tr('Закрыть')}</button>
      </div>
    </div>
    {form && <form className="mt-3 flex flex-wrap items-end gap-2" onSubmit={e => { e.preventDefault(); change.mutate(form) }}>
      <label className="grid gap-1 text-xs text-t2">{form === 'snooze' ? tr('Вернуть в очередь') : tr('Причина закрытия')}
        <input autoFocus required className="rounded-md border border-ln bg-sf px-2 py-1.5 text-sm text-tx" type={form === 'snooze' ? 'date' : 'text'} min={form === 'snooze' ? addDays(todayStr(), 1) : undefined} value={value} onChange={e => setValue(e.target.value)} />
      </label>
      <button className={btn} disabled={!value.trim() || change.isPending}>{tr('Сохранить')}</button>
      <button type="button" className={btn} disabled={change.isPending} onClick={() => setForm(null)}>{tr('Отмена')}</button>
    </form>}
    {change.isError && <p role="alert" className="mt-2 text-sm text-rd">{change.error.message}</p>}
  </li>
}

export function Attention() {
  const issues = useAttention()
  return <div className="mx-auto w-full max-w-6xl space-y-5 p-4 sm:p-8">
    <h1 className="text-2xl font-bold tracking-tight">{tr('Требует внимания')}</h1>
    {issues.isPending && <SkeletonRows />}
    {issues.isError && <ErrorState message={issues.error.message} onRetry={() => issues.refetch()} />}
    {issues.data?.length === 0 && <EmptyState icon="check" title={tr('В очереди нет открытых проблем')} />}
    {groups.map(([kind, label]) => {
      const rows = issues.data?.filter(t => (t.issue_kind || 'problem') === kind)
      if (!rows?.length) return null
      return <section key={kind} className="rounded-xl border border-ln bg-sf">
        <h2 className="flex items-center gap-2 px-4 py-3 text-base font-semibold">{tr(label)} <span className="text-xs text-t3">{rows.length}</span></h2>
        <ul>{rows.map(task => <IssueRow key={task.id} task={task} />)}</ul>
      </section>
    })}
  </div>
}
