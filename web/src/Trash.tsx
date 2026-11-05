import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, fmtStamp, plural, useProjects } from './api'
import { Icon, ProjectTile } from './icons'
import { Link, hrefWith } from './router'
import { EmptyState, ErrorState, SkeletonRows } from './ui'

export function Trash() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['trash'], queryFn: api.trash })
  const projects = useProjects()
  const pq = useQuery({ queryKey: ['trash-projects'], queryFn: api.trashProjects })
  const restoreP = useMutation({ mutationFn: (id: number) => api.restoreProject(id), onSettled: () => qc.invalidateQueries() })
  const restore = useMutation({ mutationFn: (code: string) => api.restoreTask(code), onSettled: () => qc.invalidateQueries() })
  const purge = useMutation({ mutationFn: api.purgeTrash, onSettled: () => qc.invalidateQueries() })
  const [confirm, setConfirm] = useState(false)
  useEffect(() => {
    if (!confirm) return
    const t = setTimeout(() => setConfirm(false), 4000)
    return () => clearTimeout(t)
  }, [confirm])
  const tasks = q.data ?? []
  const gone = pq.data ?? []
  const total = tasks.length + gone.length

  return (
    <>
      <header className="pagehead">
        <div className="stack">
          <h1>Корзина</h1>
          <span className="sub">{total} {plural(total, 'элемент', 'элемента', 'элементов')} · удалённое можно восстановить</span>
        </div>
        <button type="button" className={'btn' + (confirm ? ' warn' : '')} disabled={total === 0 || purge.isPending}
          onClick={() => (confirm ? (setConfirm(false), purge.mutate()) : setConfirm(true))}>
          <Icon name="trash" size={15} />{confirm ? `Удалить навсегда (${total})?` : 'Очистить корзину'}
        </button>
      </header>
      <div className="content">
        <div className="col-main">
          <section className="panel" style={{ overflow: 'hidden' }}>
            {q.isPending && <SkeletonRows n={4} />}
            {q.isError && <ErrorState message={q.error.message} onRetry={() => q.refetch()} />}
            {q.isSuccess && pq.isSuccess && total === 0 && <EmptyState icon="trash" title="Корзина пуста" hint="Сюда попадают удалённые задачи: из панели задачи или через MCP." />}
            {gone.map((p, i) => (
              <div className="row" key={'p' + p.id} style={i === 0 ? { borderTop: 0 } : undefined}>
                <span className="rowmain">
                  <ProjectTile icon={p.icon} color={p.color} size={22} radius={7} />
                  <span className="code mono">{p.key}</span>
                  <span className="title">{p.name} <span className="mc">· проект целиком</span></span>
                  {p.deleted_at && <span className="mc">удалён {fmtStamp(p.deleted_at)}</span>}
                </span>
                <button type="button" className="btn" disabled={restoreP.isPending} onClick={() => restoreP.mutate(p.id)}>Восстановить</button>
              </div>
            ))}
            {tasks.map((t, i) => {
              const p = projects.data?.find((x) => x.id === t.project_id)
              return (
                <div className="row" key={t.id} style={i === 0 && gone.length === 0 ? { borderTop: 0 } : undefined}>
                  <Link to={hrefWith('task', t.code)} className="rowmain">
                    <span className="code mono">{t.code}</span>
                    <span className="title" style={{ color: 'var(--t2)' }}>{t.title}</span>
                    {p && <span className="mc"><ProjectTile icon={p.icon} color={p.color} size={16} radius={5} />{p.name}</span>}
                    {t.deleted_at && <span className="mc">удалено {fmtStamp(t.deleted_at)}</span>}
                  </Link>
                  <button type="button" className="btn" disabled={restore.isPending} onClick={() => restore.mutate(t.code)}>Восстановить</button>
                </div>
              )
            })}
          </section>
          {restore.isError && <div className="errline" role="alert">{restore.error.message}</div>}
        </div>
      </div>
    </>
  )
}
