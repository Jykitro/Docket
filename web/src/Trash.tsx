import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errText, fmtStamp, useProjects } from './api'
import { Icon, ProjectTile } from './icons'
import { plural, tr } from './i18n'
import { Link, hrefWith } from './router'
import { ConfirmButton, EmptyState, ErrorState, SkeletonRows } from './ui'

const MC = 'inline-flex items-center gap-[5px] text-[12px] text-t3'
const ROW = 'flex items-center gap-3 min-h-[46px] px-4 py-1.5 border-ln no-underline hover:bg-sf2'
const ROWMAIN = 'flex flex-1 items-center gap-3 min-w-0 self-stretch no-underline'
const CODE = 'font-mono text-[11.5px] text-t3 w-[58px] flex-[0_0_58px]'
const BTN = 'inline-flex items-center gap-2 h-9 px-3 rounded-md border bg-sf text-sm font-medium no-underline whitespace-nowrap cursor-pointer hover:bg-sk disabled:opacity-50 disabled:cursor-default'

export function Trash() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['trash'], queryFn: api.trash })
  const projects = useProjects()
  const pq = useQuery({ queryKey: ['trash-projects'], queryFn: api.trashProjects })
  const restoreP = useMutation({ mutationFn: (id: number) => api.restoreProject(id), onSettled: () => qc.invalidateQueries() })
  const restore = useMutation({ mutationFn: (code: string) => api.restoreTask(code), onSettled: () => qc.invalidateQueries() })
  const purge = useMutation({ mutationFn: api.purgeTrash, onSettled: () => qc.invalidateQueries() })
  const tasks = q.data ?? []
  const gone = pq.data ?? []
  const total = tasks.length + gone.length
  const versions = (rows: { id: number; deleted_at: string | null }[]) =>
    rows.flatMap(row => row.deleted_at ? [{ id: row.id, deleted_at: row.deleted_at }] : [])
  const snapshot = { tasks: versions(tasks), projects: versions(gone) }
  const ready = q.isSuccess && pq.isSuccess && snapshot.tasks.length + snapshot.projects.length === total

  return (
    <>
      <header className="flex flex-wrap items-center gap-3.5 px-8 pt-6 pb-4 max-[640px]:px-4">
        <div className="flex flex-col gap-0.5 flex-[1_1_260px] min-w-0">
          <h1 className="text-2xl font-bold tracking-tight">{tr('Корзина')}</h1>
          <span className="text-sm text-t3">{plural(total, '{n} элемент|{n} элемента|{n} элементов')} · {tr('удалённое можно восстановить')}</span>
        </div>
        <ConfirmButton key={JSON.stringify(snapshot)} className={BTN + ' border-ln text-tx [&.warn]:border-rd [&.warn]:text-rd'}
          label={tr('Очистить корзину')} armed={tr('Удалить навсегда ({n})?', { n: total })}
          disabled={!ready || total === 0 || purge.isPending} icon={<Icon name="trash" size={15} />}
          onConfirm={() => purge.mutate(snapshot)} />
      </header>
      <div className="flex flex-wrap items-start gap-6 px-8 pt-6 pb-8 max-[640px]:px-4">
        <div className="flex flex-[999_1_520px] flex-col gap-[18px] min-w-0">
          <section className="overflow-hidden bg-sf border border-ln rounded-xl">
            {(q.isPending || pq.isPending) && <SkeletonRows n={4} />}
            {pq.isError && <ErrorState message={pq.error.message} onRetry={() => pq.refetch()} />}
            {q.isError && <ErrorState message={q.error.message} onRetry={() => q.refetch()} />}
            {q.isSuccess && pq.isSuccess && total === 0 && <EmptyState icon="trash" title={tr('Корзина пуста')} hint={tr('Сюда попадают удалённые задачи: из панели задачи или через MCP.')} />}
            {gone.map((p, i) => (
              <div className={ROW + (i === 0 ? ' border-t-0' : ' border-t')} key={'p' + p.id}>
                <span className={ROWMAIN}>
                  <ProjectTile project={p} size={22} radius={7} />
                  <span className={CODE}>{p.key}</span>
                  <span className="flex-[1_1_200px] min-w-0 font-medium">{p.name} <span className={MC}>· {tr('проект целиком')}</span></span>
                  {p.deleted_at && <span className={MC}>{tr('удалён {when}', { when: fmtStamp(p.deleted_at) })}</span>}
                </span>
                <button type="button" className={BTN + ' border-ln text-tx'} disabled={restoreP.isPending} onClick={() => restoreP.mutate(p.id)}>{tr('Восстановить')}</button>
              </div>
            ))}
            {tasks.map((t, i) => {
              const p = projects.data?.find((x) => x.id === t.project_id)
              return (
                <div className={ROW + (i === 0 && gone.length === 0 ? ' border-t-0' : ' border-t')} key={t.id}>
                  <Link to={hrefWith('task', t.code)} className={ROWMAIN}>
                    <span className={CODE}>{t.code}</span>
                    <span className="flex-[1_1_200px] min-w-0 font-medium text-t2">{t.title}</span>
                    {p && <span className={MC}><ProjectTile project={p} size={16} radius={5} />{p.name}</span>}
                    {t.deleted_at && <span className={MC}>{tr('удалено {when}', { when: fmtStamp(t.deleted_at) })}</span>}
                  </Link>
                  <button type="button" className={BTN + ' border-ln text-tx'} disabled={restore.isPending} onClick={() => restore.mutate(t.code)}>{tr('Восстановить')}</button>
                </div>
              )
            })}
          </section>
          {purge.isError && <div className="text-[13px] text-rd" role="alert">{errText(purge.error)}</div>}
          {restore.isError && <div className="text-[13px] text-rd" role="alert">{restore.error.message}</div>}
        </div>
      </div>
    </>
  )
}
