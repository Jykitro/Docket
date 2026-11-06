// Icon set from docs/design/icons.json plus the few UI glyphs the screens use. viewBox 24, stroke 1.75, round caps.
const P = {
  // project icons (the database stores these names)
  target: 'M12 4a8 8 0 1 0 0 16a8 8 0 0 0 0-16zM12 9a3 3 0 1 0 0 6a3 3 0 0 0 0-6zM12 2v3M12 19v3M2 12h3M19 12h3',
  clover: 'M12 12c-3-3-3-8 0-8s3 5 0 8zM12 12c3-3 8-3 8 0s-5 3-8 0zM12 12c3 3 3 8 0 8s-3-5 0-8zM12 12c-3 3-8 3-8 0s5-3 8 0z',
  key: 'M14 10a4 4 0 1 1-8 0a4 4 0 0 1 8 0zM13 12l7 7M17 16l2-2M15 18l2-2',
  stack: 'M4 6h16v4H4zM4 14h16v4H4zM7 8h.01M7 16h.01',
  candle: 'M7 4v4M7 15v4M5 8h4v7H5zM17 3v3M17 14v5M15 6h4v8h-4z',
  bars: 'M5 20V10M12 20V4M19 20v-7',
  board: 'M4 5h4v14H4zM10 5h4v9h-4zM16 5h4v12h-4z',
  plug: 'M9 3v5M15 3v5M6 8h12v3a6 6 0 0 1-12 0zM12 17v4',
  flag: 'M6 21V4M6 5h11l-2 4 2 4H6',
  tag: 'M4 12V5a1 1 0 0 1 1-1h7l8 8-8 8zM8 8h.01',
  doc: 'M7 3h8l4 4v14H7zM15 3v4h4M10 12h6M10 16h6',
  cal: 'M5 6h14a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1zM4 10h16M8 3v4M16 3v4',
  link: 'M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1',
  spark: 'M12 3v18M3 12h18M6 6l12 12M18 6L6 18',
  home: 'M4 11l8-7 8 7M6 10v10h12V10M10 20v-6h4v6',
  cart: 'M4 5h2l2 10h9l2-7H7M10 19h.01M17 19h.01',
  globe: 'M12 4a8 8 0 1 0 0 16a8 8 0 0 0 0-16zM4 12h16M12 4c3 3 3 13 0 16M12 4c-3 3-3 13 0 16',
  // UI glyphs
  clock: 'M12 4a8 8 0 1 0 0 16a8 8 0 0 0 0-16zM12 8v4l3 2',
  trash: 'M5 7h14M10 7V4h4v3M7 7l1 13h8l1-13M10 11v6M14 11v6',
  user: 'M12 4a4 4 0 1 0 0 8a4 4 0 0 0 0-8zM5 20a7 7 0 0 1 14 0',
  search: 'M11 4a7 7 0 1 0 0 14a7 7 0 0 0 0-14zM20 20l-4-4',
  plus: 'M12 5v14M5 12h14',
  x: 'M6 6l12 12M18 6L6 18',
  check: 'M5 12.5l4.5 4.5L19 7.5',
  chevL: 'M15 6l-6 6 6 6',
  chevR: 'M9 6l6 6-6 6',
  arrowRight: 'M5 12h14M13 6l6 6-6 6',
  sliders: 'M4 8h9M17 8h3M4 16h3M11 16h9M15 5v6M9 13v6',
  subtask: 'M6 5v8a3 3 0 0 0 3 3h9M14 12l4 4-4 4',
  funnel: 'M4 5h16l-6 8v6l-4-2v-4z',
  list: 'M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01',
  sun: 'M12 8a4 4 0 1 0 0 8a4 4 0 0 0 0-8zM12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4',
  moon: 'M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z',
  monitor: 'M4 5h16v11H4zM9 20h6M12 16v4',
  logo: 'M6 4h5a8 8 0 0 1 0 16H6zM9.5 12.2l2 2 3.2-4',
  chevD: 'M6 9l6 6 6-6',
  chevUD: 'M8 9l4-4 4 4M16 15l-4 4-4-4',
  panelL: 'M4 5h16v14H4zM9 5v14',
  expand: 'M14 4h6v6M20 4l-8 8M10 6H5a1 1 0 0 0-1 1v12a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-5',
} as const

export const PROJECT_ICONS = ['target', 'clover', 'key', 'stack', 'candle', 'bars', 'board', 'plug', 'flag', 'tag', 'doc', 'cal', 'link', 'spark', 'home', 'cart', 'globe'] as const
export const PROJECT_COLORS = ['#2D5CC9', '#17784C', '#B8620A', '#0E7490', '#C0265F', '#6D3FD0', '#4B5362', '#A12B2B', '#2F7D1F', '#B0791E']

/** Unknown names (e.g. an icon added later) fall back to the board glyph. */
export function Icon({ name, size = 16, sw = 1.75 }: { name: string; size?: number; sw?: number }) {
  const d = (P as Record<string, string>)[name] ?? P.board
  return (
    <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" strokeWidth={sw}
      strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={d} />
    </svg>
  )
}

/** Coloured tile with a project's icon (sidebar 20px, headers 40px). */
export function ProjectTile({ icon, color, size = 20, radius = 6 }: { icon: string; color: string; size?: number; radius?: number }) {
  return (
    <span className="grid place-items-center text-white" style={{ width: size, height: size, flex: `0 0 ${size}px`, borderRadius: radius, background: color }}>
      <Icon name={icon} size={Math.round(size * 0.6)} sw={size > 28 ? 1.9 : 2} />
    </span>
  )
}
