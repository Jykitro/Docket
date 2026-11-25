// First run of a workspace: your name, the workspace itself, the first project, then connecting an assistant.
// Steps that are already done are left out (a name that is set, existing projects). The right half is a live
// miniature of the sidebar that fills in as you type, with the part the current step edits outlined.
import { type ReactNode, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type Project, api, errText, useMe, useProfile } from './api'
import { NewProjectForm, type ProjectDraft } from './Dialogs'
import { Icon, ProjectTile } from './icons'
import { PlandilloLogo } from './PlandilloLogo'
import { ProjectIconPicker } from './ProjectIconPicker'
import { tr } from './i18n'
import { navigate } from './router'
import { Avatar, LangSwitch, btn, btnPri, cap, textInput } from './ui'
import { SPACE_NAME_MAX, SpaceIcon, useSpaces } from './Workspaces'
import { workspaceId } from './workspacePath'

export const NAME_MAX = 40

const KEY = 'plandillo-onboarded-' + workspaceId
export function isOnboarded() {
  try { return !!localStorage.getItem(KEY) } catch { return false }
}
function markOnboarded() {
  try { localStorage.setItem(KEY, '1') } catch { /* storage can be blocked: onboarding may show again in this browser */ }
}

type Step = 'you' | 'space' | 'project' | 'agent'
const STEP_LABEL: Record<Step, string> = { you: 'Вы', space: 'Пространство', project: 'Проект', agent: 'Ассистент' }

export function Onboarding({ hasProjects, onDone }: { hasProjects: boolean; onDone: () => void }) {
  const qc = useQueryClient()
  const profile = useProfile().data
  const me = useMe().data
  const space = useSpaces().data?.find((s) => s.id === workspaceId)
  // The step list is fixed on first render so that finishing a step never removes it from the progress bar.
  const [steps] = useState<Step[]>(() => [
    ...(!profile?.name ? ['you' as const] : []),
    ...(space ? ['space' as const] : []),
    ...(!hasProjects ? ['project' as const] : []),
    'agent' as const,
  ])
  const [at, setAt] = useState(0)
  const step = steps[at]
  const next = () => setAt((i) => Math.min(i + 1, steps.length - 1))
  const back = at > 0 ? () => setAt(at - 1) : undefined

  const [name, setName] = useState(profile?.name ?? '')
  const [spaceName, setSpaceName] = useState(space?.name ?? '')
  const [spaceIcon, setSpaceIcon] = useState<{ icon?: string; icon_png?: string }>({})
  const [draft, setDraft] = useState<ProjectDraft>()
  const [created, setCreated] = useState<Project>()

  const save = useMutation({
    mutationFn: async () => {
      if (step === 'you') await api.setProfile(name.trim())
      if (step === 'space' && space) await api.changeSpace(space.id, { name: spaceName.trim(), ...spaceIcon })
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: [step === 'you' ? 'profile' : 'workspaces'] })
      next()
    },
  })
  const finish = (to: string) => {
    markOnboarded()
    onDone()
    navigate(to)
  }

  const previewIcon = spaceIcon.icon_png
    ? <img alt="" className="size-8 shrink-0 rounded-lg object-cover" src={'data:image/png;base64,' + spaceIcon.icon_png} />
    : <SpaceIcon space={space && { ...space, icon: spaceIcon.icon ?? space.icon, has_icon: !spaceIcon.icon && space.has_icon }} />
  const project = created ?? (draft?.name.trim() ? { name: draft.name.trim(), icon: draft.icon, color: draft.color, icon_url: draft.png && 'data:image/png;base64,' + draft.png } : undefined)

  return (
    <main className="grid min-h-screen grid-cols-1 lg:grid-cols-2">
      <section className="flex min-h-screen flex-col px-6 py-6 sm:px-12">
        <div className="flex items-center gap-3">
          <div className="grow"><PlandilloLogo /></div>
          <LangSwitch />
        </div>

        <div className="mx-auto flex w-full max-w-[440px] grow flex-col justify-center gap-8 py-10">
          <ol className="flex gap-2" aria-label={tr('Шаг {step} из {total}', { step: at + 1, total: steps.length })}>
            {steps.map((s, i) => (
              <li key={s} className="flex grow basis-0 flex-col gap-1.5" aria-current={i === at ? 'step' : undefined}>
                <span className={'h-1 rounded-full motion-safe:transition-colors ' + (i <= at ? 'bg-ink' : 'bg-sk')} />
                <span className={'text-xs ' + (i === at ? 'font-medium text-tx' : 'text-t3')}>{tr(STEP_LABEL[s])}</span>
              </li>
            ))}
          </ol>

          {step === 'you' && (
            <StepForm title={tr('Как вас называть?')} text={tr('Имя видно рядом с задачами, заметками и историей. Так вы отличите свои действия от действий ассистента.')}
              busy={save.isPending} error={save.error} canNext={!!name.trim()} onNext={() => save.mutate()}>
              <label className="flex flex-col gap-1.5">
                <span className={cap}>{tr('Ваше имя')}</span>
                <input className={textInput + ' h-10'} autoFocus maxLength={NAME_MAX} value={name} onChange={(e) => setName(e.target.value)} />
              </label>
            </StepForm>
          )}

          {step === 'space' && (
            <StepForm title={tr('Назовите пространство')} text={tr('В пространстве живут проекты, вики и подключения агентов. Позже можно завести ещё одно, например для работы и для дома.')}
              busy={save.isPending} error={save.error} canNext={!!spaceName.trim()} onNext={() => save.mutate()} onBack={back}>
              <label className="flex flex-col gap-1.5">
                <span className={cap}>{tr('Название')}</span>
                <input className={textInput + ' h-10'} autoFocus maxLength={SPACE_NAME_MAX} value={spaceName} onChange={(e) => setSpaceName(e.target.value)} />
              </label>
              <div className="flex flex-col gap-1.5">
                <span className={cap}>{tr('Иконка')}</span>
                <ProjectIconPicker icon={spaceIcon.icon ?? space?.icon ?? 'board'} preview={spaceIcon.icon_png && 'data:image/png;base64,' + spaceIcon.icon_png}
                  onSelect={(c) => setSpaceIcon(c.icon_png ? { icon_png: c.icon_png } : { icon: c.icon })} />
              </div>
            </StepForm>
          )}

          {step === 'project' && (
            <div className="flex flex-col gap-5">
              <Heading title={tr('Первый проект')} text={tr('Проект — это доска со своими статусами и задачами. Ключ становится частью номера задачи: TRK-07.')} />
              <div className="flex flex-col gap-4">
                <NewProjectForm onDraft={setDraft} cancelLabel="Пропустить" submitLabel="Создать проект" onCancel={next} onCreated={(p) => { setCreated(p); next() }} />
              </div>
            </div>
          )}

          {step === 'agent' && (
            <div className="flex flex-col gap-6">
              <Heading title={tr('Подключите ассистента')} text={tr('Через MCP ассистент сам заводит задачи, ведёт план и отмечает сделанное, а вы видите всё здесь.')} />
              <ul className="flex flex-col gap-3 text-sm">
                {[
                  ['plug', tr('Claude Code, Claude Desktop, Codex и другие клиенты MCP')],
                  ['key', tr('Вход через браузер, без ручной передачи токена')],
                  ['check', tr('Каждое изменение агента видно в истории задачи')],
                ].map(([icon, text]) => (
                  <li key={icon} className="flex items-center gap-3">
                    <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-acs text-act"><Icon name={icon} size={16} sw={2} /></span>{text}
                  </li>
                ))}
              </ul>
              <div className="flex flex-wrap items-center justify-end gap-2">
                <button type="button" className={btn} onClick={() => finish(created ? `/p/${created.slug}` : '/')}>{tr('Позже')}</button>
                <button type="button" className={btnPri} onClick={() => finish('/integrations')}>{tr('Подключить ассистента')}<Icon name="arrowRight" size={15} sw={2} /></button>
              </div>
            </div>
          )}
        </div>
      </section>

      <aside className="relative hidden items-center justify-center overflow-hidden border-l border-ln bg-acs lg:flex" aria-hidden="true">
        <div className="flex w-[300px] flex-col gap-4">
          <div className="flex flex-col rounded-xl border border-ln bg-sf p-2 shadow-pop">
            <Spot on={step === 'space'}>
              <div className="flex items-center gap-2.5 p-1.5">
                {previewIcon}
                <span className="flex min-w-0 flex-col leading-tight">
                  <b className="truncate text-sm font-semibold">{(step === 'space' ? spaceName : space?.name) || tr('Пространство')}</b>
                  <span className="truncate text-xs text-t3">{name || tr('Рабочее пространство')}</span>
                </span>
              </div>
            </Spot>
            <div className={cap + ' px-2 pt-3 pb-1'}>{tr('Общее')}</div>
            {[['cal', tr('Сегодня')], ['flag', tr('Требует внимания')], ['plug', tr('Интеграции')]].map(([icon, label]) => (
              <div key={icon} className="flex h-8 items-center gap-2 px-2 text-sm text-t2">
                <Icon name={icon} />{label}
                {icon === 'plug' && step === 'agent' && <span className="ml-auto size-[7px] rounded-full bg-dn" />}
              </div>
            ))}
            <div className={cap + ' px-2 pt-3 pb-1'}>{tr('Проекты')}</div>
            <Spot on={step === 'project'}>
              {project
                ? <div className="flex h-8 items-center gap-2 px-2 text-sm font-medium"><ProjectTile project={project} /><span className="truncate">{project.name}</span></div>
                : <div className="flex h-8 items-center gap-2 px-2 text-sm text-t3"><span className="size-5 rounded-md border border-dashed border-ln2" />{tr('Первый проект')}</div>}
            </Spot>
            <div className="mt-6 border-t border-ln pt-2">
              <Spot on={step === 'you'}>
                <div className="flex items-center gap-2 p-1.5">
                  <Avatar name={name} photo={me?.user?.photo && me.user.photo + '?v=' + me.user.updated_at} />
                  <span className="flex min-w-0 flex-col leading-tight">
                    <b className="truncate text-sm font-semibold">{name || tr('Ваше имя')}</b>
                    <span className="truncate text-xs text-t3">{me?.user?.username ?? tr('локально · без входа')}</span>
                  </span>
                </div>
              </Spot>
            </div>
          </div>
          <p className="text-center text-[13px] text-t2">{tr('Так пространство будет выглядеть в боковой панели.')}</p>
        </div>
      </aside>
    </main>
  )
}

/** Outlines the part of the preview that the current step changes. */
function Spot({ on, children }: { on: boolean; children: ReactNode }) {
  return <div className={'rounded-lg motion-safe:transition-shadow ' + (on ? 'ring-2 ring-ink/50 ring-offset-2 ring-offset-sf' : '')}>{children}</div>
}

function Heading({ title, text }: { title: string; text: string }) {
  return (
    <div className="flex flex-col gap-2">
      <h1 className="text-[28px] leading-tight font-semibold tracking-[-.02em]">{title}</h1>
      <p className="text-[15px] text-t2">{text}</p>
    </div>
  )
}

function StepForm({ title, text, busy, error, canNext, onNext, onBack, children }: {
  title: string; text: string; busy: boolean; error: Error | null; canNext: boolean; onNext: () => void; onBack?: () => void; children: ReactNode
}) {
  return (
    <form className="flex flex-col gap-6" onSubmit={(e) => { e.preventDefault(); if (canNext && !busy) onNext() }}>
      <Heading title={title} text={text} />
      <div className="flex flex-col gap-4">{children}</div>
      {error && <p className="text-[13px] text-rd" role="alert">{errText(error)}</p>}
      <div className="flex items-center gap-2">
        {onBack && <button type="button" className={btn} onClick={onBack}><Icon name="chevL" size={15} />{tr('Назад')}</button>}
        <button type="submit" className={btnPri + ' ml-auto'} disabled={!canNext || busy}>{tr('Продолжить')}<Icon name="arrowRight" size={15} sw={2} /></button>
      </div>
    </form>
  )
}
