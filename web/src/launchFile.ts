// A Markdown file opened with the installed app (manifest file_handlers → /?open=import) arrives in this document's
// launchQueue. The address has no workspace, so WorkspaceGate replaces the document: keep the file in sessionStorage
// (same tab) until the import dialog takes it.
const KEY = 'plandillo-import'
export type ImportFile = { name: string; text: string }

type LaunchQueue = { setConsumer: (consume: (p: { files?: { getFile: () => Promise<File> }[] }) => void) => void }
let arrived: Promise<void> | undefined

/** Call once at startup, before WorkspaceGate can leave the document. */
export function watchLaunchFiles() {
  const queue = (window as Window & { launchQueue?: LaunchQueue }).launchQueue
  if (!queue) return
  arrived = new Promise((resolve) => queue.setConsumer(async (p) => {
    try {
      const file = await p.files?.[0]?.getFile()
      if (file) sessionStorage.setItem(KEY, JSON.stringify({ name: file.name, text: await file.text() }))
    } catch { /* unreadable file or blocked storage: the dialog offers the file picker instead */ }
    resolve()
  }))
}

/** Resolves when a launched file is stored, at once for any other address; 1.5 s at most. */
export function launchSettled(): Promise<unknown> {
  if (!arrived || new URLSearchParams(location.search).get('open') !== 'import') return Promise.resolve()
  return Promise.race([arrived, new Promise((r) => setTimeout(r, 1500))])
}

/** The launched file, once: it is removed so a reload does not import it again. */
export function takeLaunchFile(): ImportFile | undefined {
  try {
    const raw = sessionStorage.getItem(KEY)
    sessionStorage.removeItem(KEY)
    return raw ? JSON.parse(raw) as ImportFile : undefined
  } catch { return undefined }
}
