// TanStack Router with the route tree from routes.ts. Pages keep using these helpers with plain paths such as /p/<slug>?view=3.
import { type ComponentProps, type MouseEvent } from 'react'
import { createRouter, useRouterState } from '@tanstack/react-router'

import { routeTree } from './routeTree.gen'
import { ErrorPage } from './ui'
import { tr } from './i18n'
import { appPath, workspacePath } from './workspacePath'

export const router = createRouter({
  routeTree,
  // Public URLs drop /p/ and carry /w/<id>; the route tree sees app paths.
  rewrite: {
    input: ({ url }) => { url.pathname = appPath(url.pathname); return url },
    output: ({ url }) => { url.pathname = workspacePath(url.pathname); return url },
  },
  // Plain query strings (?task=TRK-07), not the default JSON values.
  parseSearch: (s) => Object.fromEntries(new URLSearchParams(s)),
  stringifySearch: (o) => { const q = new URLSearchParams(o as Record<string, string>).toString(); return q ? '?' + q : '' },
  defaultPreload: 'intent',
  defaultNotFoundComponent: () => <ErrorPage kind="not_found" title={tr('Страница не найдена')} hint={tr('По адресу {path} ничего нет. Возможно, ссылка устарела или страницу удалили.', { path: location.pathname })} />,
})

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}

export function useRoute() {
  const s = useRouterState({ select: (st) => st.location.pathname + st.location.searchStr })
  const [path, search = ''] = s.split('?')
  return { path, params: new URLSearchParams(search) }
}

/** Goes to an app path (/p/<slug>) or a public URL; a change of the query alone keeps the scroll position. */
export function navigate(to: string, replace = false) {
  const href = workspacePath(to)
  if (href === location.pathname + location.search) return
  const samePage = appPath(new URL(href, location.origin).pathname) === router.state.location.pathname
  void router.navigate({ href, replace, resetScroll: !samePage })
}

export function scrollToSection(element: HTMLElement | null) {
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  element?.scrollIntoView({ behavior: reducedMotion ? 'instant' : 'smooth', block: 'start' })
}

/** Sets or removes one query parameter of the current URL (null removes it). */
export function setParam(key: string, value: string | null, replace = true) {
  navigate(hrefWith(key, value), replace)
}

/** The current URL with one query parameter set (null removes it), for links such as ?task=TRK-07. */
export function hrefWith(key: string, value: string | null) {
  const p = new URLSearchParams(location.search)
  if (value === null) p.delete(key)
  else p.set(key, value)
  const q = p.toString()
  // An app path: Link and navigate add the workspace prefix themselves.
  return appPath(location.pathname) + (q ? '?' + q : '')
}

export function Link({ to, onClick, onMouseEnter, onFocus, ...rest }: { to: string } & ComponentProps<'a'>) {
  const href = workspacePath(to)
  const click = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e)
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    e.preventDefault()
    navigate(to)
  }
  // Loads the page's chunk before the click. buildLocation reads href alone; the type insists on `to`.
  const preload = () => { router.preloadRoute({ href } as Parameters<typeof router.preloadRoute>[0]).catch(() => {}) }
  return <a href={href} onClick={click} onMouseEnter={(e) => { onMouseEnter?.(e); preload() }} onFocus={(e) => { onFocus?.(e); preload() }} {...rest} />
}
