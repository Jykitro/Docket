// First run (the profile has no name yet): step 1 asks for a name, step 2 creates the first project.
// Step 2 is skipped when the database already has projects.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import { NewProjectForm } from './Dialogs'
import { Icon } from './icons'
import { tr } from './i18n'
import { navigate } from './router'
import { LangSwitch, btnPri, cap } from './ui'

export const NAME_MAX = 40

export function Onboarding({ hasProjects, onDone }: { hasProjects: boolean; onDone: () => void }) {
  const qc = useQueryClient()
  const [step, setStep] = useState<1 | 2>(1)
  const [name, setName] = useState('')
  const save = useMutation({
    mutationFn: () => api.setProfile(name.trim()),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['profile'] })
      if (hasProjects) onDone()
      else setStep(2)
    },
  })
  const submit = () => name.trim() && !save.isPending && save.mutate()

  return (
    <main className="min-h-screen grid place-items-center px-4 py-8">
      <div className="w-full max-w-[460px] bg-sf border border-ln rounded-lg shadow-pop p-6 flex flex-col gap-4">
        <div className="flex items-center gap-2.5">
          <span className="size-8 rounded-md bg-ink text-inkt grid place-items-center"><Icon name="logo" size={19} sw={2} /></span>
          <b className="text-[17px] font-bold tracking-[-.01em] grow">Docket</b>
          <LangSwitch />
          {!hasProjects && <span className={cap}>{tr('Шаг {step} из 2', { step })}</span>}
        </div>
        {step === 1 ? (
          <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); submit() }}>
            <div className="flex flex-col gap-1">
              <h1 className="text-[20px] font-semibold tracking-[-.01em]">{tr('Добро пожаловать')}</h1>
              <p className="text-sm text-t2">{tr('Docket работает локально и без входа. Как вас зовут? Имя увидите вы и те, кто пользуется этой копией: оно подписывает ваши задачи, заметки и историю.')}</p>
            </div>
            <label className="flex flex-col gap-1.5">
              <span className={cap}>{tr('Ваше имя')}</span>
              <input className="h-[38px] border border-ln rounded-md bg-sf px-3 text-[14px] text-tx focus:border-ring focus:ring-[3px] focus:ring-ring/50 focus:outline-none" autoFocus maxLength={NAME_MAX} value={name} onChange={(e) => setName(e.target.value)} />
            </label>
            {save.isError && <div className="text-rd text-[13px]" role="alert">{save.error.message}</div>}
            <div className="flex justify-end">
              <button type="submit" className={btnPri} disabled={!name.trim() || save.isPending}>{tr('Продолжить')}<Icon name="arrowRight" size={15} sw={2} /></button>
            </div>
          </form>
        ) : (
          <>
            <div className="flex flex-col gap-1">
              <h1 className="text-[20px] font-semibold tracking-[-.01em]">{tr('Первый проект')}</h1>
              <p className="text-sm text-t2">{tr('Проект — это доска со своими статусами и задачами. Всё остальное можно поменять позже в его настройках.')}</p>
            </div>
            <NewProjectForm cancelLabel="Позже" onCancel={onDone} onCreated={(p) => { onDone(); navigate(`/p/${p.slug}`) }} />
          </>
        )}
      </div>
    </main>
  )
}
