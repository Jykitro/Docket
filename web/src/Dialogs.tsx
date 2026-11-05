// "New task" (key C) and "New project" dialogs. Pickers are chips and segmented buttons, not native selects.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type Project, addDays, api, fmtDay, todayStr, useProjectsMeta } from './api'
import { Icon, PROJECT_COLORS, PROJECT_ICONS, ProjectTile } from './icons'
import { navigate } from './router'
import { Dialog, btn, btnPri, cap } from './ui'

const field = 'flex flex-col gap-1.5'
const chips = 'flex flex-wrap gap-1.5'
const choice = 'inline-flex items-center gap-1.5 h-7 px-2.5 rounded-full border text-[12.5px] '
const choiceOn = choice + 'bg-acs border-transparent text-act font-medium'
const choiceOff = choice + 'bg-sf border-ln text-t2 hover:bg-sk'
const dateinput = 'h-7 border border-ln rounded-lg bg-sf px-2 text-[12.5px] text-tx focus:border-ac focus:outline-none'
const textin = 'h-[34px] border border-ln rounded-lg bg-sf px-2.5 text-[13.5px] text-tx focus:border-ac focus:outline-none'
const errline = 'text-rd text-[13px]'
const foot = 'flex items-center gap-2 justify-end'
const pick = 'border bg-sf p-0 aria-pressed:border-ac '

export function NewTaskDialog({ projects, project, date, status, title: initial = '', onClose }: {
  projects: Project[]; project?: Project; date?: string; status?: string; title?: string; onClose: () => void
}) {
  const qc = useQueryClient()
  const [pid, setPid] = useState((project ?? projects[0])?.id)
  const [title, setTitle] = useState(initial)
  const [st, setSt] = useState(status ?? '')
  const [prio, setPrio] = useState('P1')
  const [area, setArea] = useState('')
  const [day, setDay] = useState(date ?? '')
  const meta = useProjectsMeta(pid ? [pid] : [])[pid ?? 0]
  const proj = projects.find((p) => p.id === pid)
  const statuses = meta?.statuses ?? []
  const effStatus = statuses.some((s) => s.key === st) ? st : (statuses.find((s) => s.key === 'backlog') ?? statuses[0])?.key ?? ''

  const create = useMutation({
    mutationFn: () => api.createTask({ project: proj!.key, title: title.trim(), status: effStatus, priority: prio, area: area.trim(), planned_for: day }),
    onSuccess: () => {
      qc.invalidateQueries()
      onClose()
    },
  })
  const submit = () => title.trim() && proj && !create.isPending && create.mutate()
  const tomorrow = addDays(todayStr(), 1)

  return (
    <Dialog title="Новая задача" onClose={onClose}>
      <input className="border-b border-ln bg-transparent text-[17px] font-medium pt-1.5 pb-2.5 px-0 outline-none w-full focus:border-ac" autoFocus placeholder="Что нужно сделать" aria-label="Название задачи" value={title}
        onChange={(e) => setTitle(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && submit()} />
      <div className={field}>
        <span className={cap}>Проект</span>
        <div className={chips}>
          {projects.map((p) => (
            <button key={p.id} type="button" className={p.id === pid ? choiceOn : choiceOff} onClick={() => { setPid(p.id); setSt(''); setArea('') }}>
              <ProjectTile icon={p.icon} color={p.color} size={16} radius={5} />{p.name}
            </button>
          ))}
        </div>
      </div>
      <div className={field}>
        <span className={cap}>Статус</span>
        <div className={chips}>
          {statuses.map((s) => (
            <button key={s.id} type="button" className={s.key === effStatus ? choiceOn : choiceOff} onClick={() => setSt(s.key)}>{s.name}</button>
          ))}
        </div>
      </div>
      <div className={field}>
        <span className={cap}>Приоритет</span>
        <div className={chips}>
          {['P0', 'P1', 'P2'].map((p) => (
            <button key={p} type="button" className={(p === prio ? choiceOn : choiceOff) + ' font-mono'} onClick={() => setPrio(p)}>{p}</button>
          ))}
        </div>
      </div>
      <div className={field}>
        <span className={cap}>Область</span>
        <div className={chips}>
          {(meta?.labels ?? []).map((l) => (
            <button key={l.id} type="button" className={l.name === area ? choiceOn : choiceOff} onClick={() => setArea(l.name === area ? '' : l.name)}>
              <span className="inline-block w-[7px] h-[7px] rounded-full flex-[0_0_7px]" style={{ background: l.color }} />{l.name}
            </button>
          ))}
          <input className={dateinput + ' w-[130px]'} placeholder="Новая область" aria-label="Новая область" value={area}
            onChange={(e) => setArea(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && submit()} />
        </div>
      </div>
      <div className={field}>
        <span className={cap}>Дата</span>
        <div className={chips}>
          {[todayStr(), tomorrow].map((d) => (
            <button key={d} type="button" className={day === d ? choiceOn : choiceOff} onClick={() => setDay(d)}>{fmtDay(d)}</button>
          ))}
          <button type="button" className={day === '' ? choiceOn : choiceOff} onClick={() => setDay('')}>Без даты</button>
          <input className={dateinput} type="date" aria-label="Выбрать дату" value={day} onChange={(e) => setDay(e.target.value)} />
        </div>
      </div>
      {create.isError && <div className={errline} role="alert">{create.error.message}</div>}
      <div className={foot}>
        <button type="button" className={btn} onClick={onClose}>Отмена</button>
        <button type="button" className={btnPri} disabled={!title.trim() || !proj || create.isPending} onClick={submit}>Создать</button>
      </div>
    </Dialog>
  )
}

export function NewProjectDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const [key, setKey] = useState('')
  const [icon, setIcon] = useState('board')
  const [color, setColor] = useState(PROJECT_COLORS[0])
  const create = useMutation({
    mutationFn: () => api.createProject({ name: name.trim(), key: key.trim() || undefined, icon, color }),
    onSuccess: (p) => {
      qc.invalidateQueries()
      onClose()
      navigate(`/p/${p.slug}`)
    },
  })
  const submit = () => name.trim() && !create.isPending && create.mutate()
  return (
    <Dialog title="Новый проект" onClose={onClose}>
      <div className={field}>
        <span className={cap}>Название</span>
        <input className={textin + ' w-full'} autoFocus value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && submit()} aria-label="Название проекта" />
      </div>
      <div className={field}>
        <span className={cap}>Ключ (2–5 символов, например TRK)</span>
        <input className={textin + ' font-mono w-[120px]'} maxLength={5} value={key} placeholder="авто"
          onChange={(e) => setKey(e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, ''))} onKeyDown={(e) => e.key === 'Enter' && submit()} aria-label="Ключ проекта" />
      </div>
      <div className={field}>
        <span className={cap}>Иконка</span>
        <div className="grid grid-cols-[repeat(9,1fr)] gap-1.5">
          {PROJECT_ICONS.map((i) => (
            <button key={i} type="button" className={pick + 'h-8 rounded-lg grid place-items-center border-ln text-t2 aria-pressed:bg-acs aria-pressed:text-act'} aria-label={i} aria-pressed={i === icon} onClick={() => setIcon(i)}><Icon name={i} size={16} /></button>
          ))}
        </div>
      </div>
      <div className={field}>
        <span className={cap}>Цвет</span>
        <div className="flex flex-wrap gap-2">
          {PROJECT_COLORS.map((c) => (
            <button key={c} type="button" className="size-7 rounded-full border-2 border-transparent p-0 aria-pressed:border-tx" style={{ background: c }} aria-label={c} aria-pressed={c === color} onClick={() => setColor(c)} />
          ))}
        </div>
      </div>
      {create.isError && <div className={errline} role="alert">{create.error.message}</div>}
      <div className={foot}>
        <button type="button" className={btn} onClick={onClose}>Отмена</button>
        <button type="button" className={btnPri} disabled={!name.trim() || create.isPending} onClick={submit}>Создать</button>
      </div>
    </Dialog>
  )
}
