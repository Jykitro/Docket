import { useRef, useState } from 'react'
import { Icon, PROJECT_ICONS } from './icons'
import { tr } from './i18n'
import { btn } from './ui'

const formats = ['image/png', 'image/jpeg', 'image/webp', 'image/svg+xml']

async function thumbnail(file: File): Promise<string> {
  if (!formats.includes(file.type) || file.size > 2 * 1024 * 1024) {
    throw new Error(tr('Выберите PNG, JPEG, WebP или SVG до 2 МБ.'))
  }
  const url = URL.createObjectURL(file)
  try {
    // Keep SVG in an image context: its document never enters the page.
    const img = new Image()
    img.src = url
    await img.decode()
    const canvas = document.createElement('canvas')
    canvas.width = canvas.height = 128
    const context = canvas.getContext('2d')
    if (!context || !img.naturalWidth || !img.naturalHeight) throw new Error()
    const scale = 128 / Math.max(img.naturalWidth, img.naturalHeight)
    const w = img.naturalWidth * scale
    const h = img.naturalHeight * scale
    context.drawImage(img, (128 - w) / 2, (128 - h) / 2, w, h)
    return canvas.toDataURL('image/png').split(',')[1]
  } catch {
    throw new Error(tr('Не удалось прочитать изображение.'))
  } finally {
    URL.revokeObjectURL(url)
  }
}

type Props = {
  icon: string
  preview?: string
  disabled?: boolean
  onBusy?: (busy: boolean) => void
  onSelect: (change: { icon?: string; icon_png?: string }) => Promise<unknown> | void
}

export function ProjectIconPicker({ icon, preview, disabled, onBusy, onSelect }: Props) {
  const input = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function choose(selection: string | File) {
    setBusy(true)
    onBusy?.(true)
    setError('')
    try {
      await onSelect(typeof selection === 'string' ? { icon: selection } : { icon_png: await thumbnail(selection) })
    } catch (e) {
      setError(e instanceof Error ? e.message : tr('Не удалось прочитать изображение.'))
    } finally {
      setBusy(false)
      onBusy?.(false)
    }
  }
  return (
    <fieldset disabled={disabled || busy} className="flex min-w-0 flex-col gap-2 border-0 p-0" aria-label={tr('Иконка проекта')}>
      <div className="flex flex-wrap gap-1.5">
        {PROJECT_ICONS.map((name) => (
          <button key={name} type="button" aria-label={name} aria-pressed={!preview && name === icon}
            className="grid size-9 place-items-center rounded-lg border border-ln bg-sf text-t2 hover:bg-sk aria-pressed:border-ac aria-pressed:bg-ac aria-pressed:text-inkt"
            onClick={() => choose(name)}><Icon name={name} size={18} /></button>
        ))}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {preview && <img src={preview} alt={tr('Иконка проекта')} className="size-9 rounded-md border border-ln object-contain" />}
        <button type="button" className={btn} onClick={() => input.current?.click()}><Icon name="plus" />{tr('Загрузить иконку')}</button>
        <span className="text-xs text-t3">{tr('PNG, JPEG, WebP или SVG до 2 МБ')}</span>
        <input ref={input} type="file" accept={formats.join(',')} hidden onChange={(e) => {
          const file = e.target.files?.[0]
          e.target.value = ''
          if (file) void choose(file)
        }} />
      </div>
      {error && <span role="alert" className="text-sm text-rd">{error}</span>}
    </fieldset>
  )
}
