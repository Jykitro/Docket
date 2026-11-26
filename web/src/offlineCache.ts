import type { PersistedClient, Persister } from '@tanstack/react-query-persist-client'

// Bump by hand when a stored response changes shape: older entries are then dropped instead of restored.
const OFFLINE_DATA_VERSION = '1'
export const OFFLINE_MAX_AGE = 7 * 24 * 60 * 60 * 1000

// Only the first key element decides: nothing like passkeys, devices, totp, admin, oauth-* or mcp-* reaches the disk.
const PERSISTED = new Set(['me', 'profile', 'workspaces', 'projects', 'today', 'attention', 'tasks', 'task', 'task-mini', 'task-pages', 'statuses', 'labels', 'fields', 'views', 'pages', 'page', 'search', 'trash', 'trash-projects'])

const DB = 'plandillo-cache'
const STORE = 'clients'
const WRITE_DELAY = 1000
// Set by the full wipe (sign-out, invalid session): a write still pending then must not bring the data back.
let wiped = false

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 1)
    req.onupgradeneeded = () => req.result.createObjectStore(STORE)
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
    req.onblocked = () => reject(new Error('IndexedDB blocked'))
  })
}

// One transaction; any failure (private mode, quota, no indexedDB) resolves with `fallback`: persistence must never break the app.
async function run<T>(mode: IDBTransactionMode, fallback: T, work: (store: IDBObjectStore) => IDBRequest<T> | void): Promise<T> {
  try {
    const db = await open()
    try {
      return await new Promise<T>((resolve, reject) => {
        const tx = db.transaction(STORE, mode)
        const req = work(tx.objectStore(STORE))
        tx.oncomplete = () => resolve(req ? req.result : fallback)
        tx.onerror = tx.onabort = () => reject(tx.error)
      })
    } finally { db.close() }
  } catch { return fallback }
}

function createPersister(key: () => string): Persister {
  let timer: ReturnType<typeof setTimeout> | undefined
  let latest: PersistedClient | undefined
  return {
    // Every cache change calls this: write the newest snapshot at most once a second.
    persistClient: client => {
      // A refetch that failed (server down) keeps its data: store it as successful, because restored as an error
      // the query would lose its place in the next snapshot and screens that check isError would hide the data.
      latest = { ...client, clientState: { ...client.clientState, queries: client.clientState.queries.map(q => q.state.status === 'error'
        ? { ...q, state: { ...q.state, status: 'success', error: null, fetchFailureCount: 0, fetchFailureReason: null } } : q) } }
      timer ??= setTimeout(() => {
        timer = undefined
        // The key is read at write time: a sign-in in this document changes the stamp without a reload.
        if (!wiped) void run('readwrite', undefined, s => { s.put(latest, key()) })
      }, WRITE_DELAY)
    },
    restoreClient: () => run<PersistedClient | undefined>('readonly', undefined, s => s.get(key())),
    removeClient: () => run('readwrite', undefined, s => { s.delete(key()) }),
  }
}

// Drops every entry that is not `${keepStamp}:…` (another sign-in's data); with no argument, all of them.
export function wipeOfflineCache(keepStamp?: string): Promise<void> {
  if (keepStamp === undefined) wiped = true
  return run('readwrite', undefined, s => {
    s.openCursor().onsuccess = e => {
      const cursor = (e.target as IDBRequest<IDBCursor | null>).result
      if (!cursor) return
      if (keepStamp === undefined || !String(cursor.key).startsWith(`${keepStamp}:`)) cursor.delete()
      cursor.continue()
    }
  })
}

export function persistOptions(key: () => string) {
  return {
    persister: createPersister(key),
    maxAge: OFFLINE_MAX_AGE,
    buster: OFFLINE_DATA_VERSION,
    // Any query holding data, also one whose last refetch failed; restored entries get a fresh gc timer, so the age is checked here.
    dehydrateOptions: { shouldDehydrateQuery: (q: { state: { data: unknown; dataUpdatedAt: number }; queryKey: readonly unknown[] }) =>
      q.state.data !== undefined && Date.now() - q.state.dataUpdatedAt < OFFLINE_MAX_AGE && PERSISTED.has(String(q.queryKey[0])) },
  }
}
