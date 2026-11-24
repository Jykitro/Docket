import { useSyncExternalStore } from 'react'
import { tr } from './i18n'

type Pending = { rows: number; label: string; finish: (proceed: boolean) => void }
const listeners = new Set<() => void>()
let pending: Pending[] = []
function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}
function update(items: Pending[]) {
  pending = items
  for (const listener of listeners) listener()
}

export class CollectionLimit extends Error {}

export class CollectionBudget {
  private bytes = 0
  private requests = 0
  private approvedBytes = 0

  async before(url: string, rows: number, signal: AbortSignal) {
    signal.throwIfAborted()
    if (this.requests >= 64) throw this.limit()
    if (this.requests > 0 && (this.requests % 5 === 0 || this.bytes - this.approvedBytes >= 4 * 1024 * 1024)) {
      const label = url.startsWith('/api/trash') ? tr('Корзина') : url.startsWith('/api/today') ? tr('Сегодня')
        : url.includes('/pages') ? tr('Вики') : url.startsWith('/api/projects') ? tr('Проекты') : tr('Задачи')
      await new Promise<void>((resolve, reject) => {
        const stop = () => item.finish(false)
        const item: Pending = { rows, label, finish(proceed) {
          signal.removeEventListener('abort', stop)
          update(pending.filter(other => other !== item))
          if (proceed) resolve()
          else reject(new CollectionLimit(tr('Загрузка списка остановлена.')))
        } }
        signal.addEventListener('abort', stop, { once: true })
        update([...pending, item])
      })
      this.approvedBytes = this.bytes
    }
    this.requests++
  }

  consume(bytes: number) {
    this.bytes += bytes
    if (this.bytes > 32 * 1024 * 1024) throw this.limit()
  }

  private limit() {
    return new CollectionLimit(tr('Список слишком велик для одного экрана. Используйте поиск или выгрузите данные через экспорт.'))
  }
}

export function PagingRequests() {
  const [item] = useSyncExternalStore(subscribe, () => pending)
  if (!item) return null
  return <aside aria-live="polite" className="fixed bottom-4 right-4 z-[100] max-w-[calc(100vw-2rem)] rounded-lg border border-ln bg-sf p-4 text-sm text-tx shadow-lg">
    <p>{tr('Загружено строк для раздела «{name}»: {n}.', { name: item.label, n: item.rows })}</p>
    <div className="mt-3 flex gap-4">
      <button type="button" className="text-act" onClick={() => item.finish(true)}>{tr('Продолжить загрузку')}</button>
      <button type="button" onClick={() => item.finish(false)}>{tr('Остановить')}</button>
    </div>
  </aside>
}
