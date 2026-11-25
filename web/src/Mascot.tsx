// The Plandillo armadillo in small scenes for error screens. Drawn from the logo path, so it follows the theme.
import { useState } from 'react'
import type { ErrorKind } from './api'
import { tr } from './i18n'

// The armadillo from public/plandillo-icon.svg (the check in the shell and the eye are holes). Spans x 55-625, y 84-362.
const BODY = 'M55 311C55 308 58 306 62 307C83 308 106 300 125 280C126 279 126.5 277.5 126.5 275C130 203 180 126 251 104C313 84 370 91 416 123C417.5 124 417.5 124.6 415.3 125.3C367.5 144 319 184 275 229.5C256 210 233 194.5 217 194.5C206 194.5 196 207 195.6 216C195.4 219.5 197 222.1 200.5 225.2C216 238.1 240.5 263.8 257.7 281.6C265.2 289.5 271.6 291.2 280 281.6C329 224.8 391 161.8 436.6 142.8C451.6 159.6 461 176 468.6 194.3L473.7 193.3C465 169 474.5 149.8 493.4 137.8C503.3 131.6 513.4 129.5 515.7 135.3C521.8 154.1 520.9 181.1 503.3 195.3C518.9 201.1 532 212.5 543.7 227.7C565.9 256.8 592.2 272.7 615.6 275.5C621.7 276.2 624.5 279.6 624.3 285.4C624 292.3 619.8 296.2 612.6 297.7C586.7 303.1 560.1 301.5 535.2 295.6C496 286.1 474.3 279.2 450 297.5C461.3 305.7 468.7 316.2 475.2 328.1C477.5 331.9 482.1 333.9 490.3 336.1C498 338.5 501.4 343.7 500.8 350.5C500.1 358 495.7 362.1 486.2 362.1L457.3 362.1C447.4 362.1 441.8 358.6 437.3 348.1C429.4 331.3 419.1 322.9 405.8 317C402.6 322 397.9 330.5 395.4 337.5C393.5 342.4 395.4 343.9 399.4 346.3C407 351.5 403.4 362.1 395.4 362.1L366.2 362.1C356.6 362.1 354 355.8 355.9 347C357.8 337.9 362.7 326.6 367.8 315C340.4 318.8 311.4 319.3 286.9 318.9C286.2 324 285.4 330.9 288.7 334.1C292 338.2 299.7 340.7 302.9 345.8C306.8 352.4 304.8 362.1 293.6 362.1L269 362.1C258.1 362.1 254 357 249.9 347.8C246.1 339.4 243 329 241 318.6L228 317.5C220.9 324.7 210.7 331 203.2 335.7C197.9 339.2 196.5 343 200.7 346.1C207 351.4 205.4 362.1 194.7 362.1L163.4 362.1C152.7 362.1 147.7 355.4 149.7 344.6C151.6 334.1 159.5 319.9 164.3 309.3L157 306.3C135.6 320.2 114 327 91.9 326.5C72.7 326 55 321.5 55 311ZM503.6 244C503.6 238.7 506.6 234.1 512.3 233.9C519.2 233.8 525.4 240.8 525.2 248.2C525.2 253.4 523.7 255.2 519.4 255C510.9 254.5 503 253.6 503.6 244Z'

// Tile colours (theme tokens) and the meme line under the picture.
const SCENES: Record<ErrorKind, { bg: string; fg: string; caption: string }> = {
  offline: { bg: 'sk', fg: 't2', caption: 'Кто-то выдернул шнур' },
  auth: { bg: 'acs', fg: 'act', caption: 'Броненосец задремал, и сессия тоже' },
  forbidden: { bg: 'acs', fg: 'act', caption: 'Сюда броненосцам без пропуска нельзя' },
  not_found: { bg: 'sk', fg: 't2', caption: 'Искали везде. Даже под панцирем' },
  rate: { bg: 'dgs', fg: 'dgt', caption: 'Помедленнее, у нас лапки' },
  server: { bg: 'rds', fg: 'rd', caption: 'Сервер лёг лапками кверху' },
  other: { bg: 'rds', fg: 'rd', caption: 'Что-то пошло не по плану' },
}

const star = (x: number, y: number, r: number) => `M${x} ${y - r}L${x + r / 3} ${y - r / 3}L${x + r} ${y}L${x + r / 3} ${y + r / 3}L${x} ${y + r}L${x - r / 3} ${y + r / 3}L${x - r} ${y}L${x - r / 3} ${y - r / 3}Z`
const drop = (x: number, y: number) => `M${x} ${y}c-9 14 -14 22 -14 30a14 14 0 0 0 28 0c0 -8 -5 -16 -14 -30Z`

/** What sits around the armadillo in each scene; `hole` is the tile colour, for cut-outs. */
function Props({ kind, hole }: { kind: ErrorKind; hole: string }) {
  const bold = { fontWeight: 800, fontFamily: 'inherit' }
  switch (kind) {
    case 'offline': return <g className="dillo-float">
      <path d="M58 312C25 340 30 398 110 398" fill="none" stroke="currentColor" strokeWidth={10} strokeLinecap="round" />
      <rect x={108} y={382} width={44} height={32} rx={7} fill="currentColor" />
      <rect x={150} y={388} width={20} height={6} rx={2} fill="currentColor" /><rect x={150} y={402} width={20} height={6} rx={2} fill="currentColor" />
      <rect x={212} y={376} width={42} height={44} rx={9} fill="currentColor" opacity={0.45} />
      <path d="M182 372l8 -14M190 392h14M182 414l8 14" stroke="currentColor" strokeWidth={6} strokeLinecap="round" />
    </g>
    case 'auth': return <>
      <path d="M503 244c0 -6 4 -10 10 -10c7 0 12 7 12 14c0 6 -2 7 -6 7c-9 -1 -16 -2 -16 -11Z" fill="currentColor" />
      <path d="M505 246q9 7 18 0" fill="none" stroke={hole} strokeWidth={5} strokeLinecap="round" />
      <g className="dillo-float" style={bold} fill="currentColor">
        <text x={600} y={190} fontSize={60}>Z</text><text x={650} y={130} fontSize={44}>z</text><text x={690} y={82} fontSize={32}>z</text>
      </g>
    </>
    case 'forbidden': return <g className="dillo-float">
      <path d="M668 262v-28a26 26 0 0 1 52 0v28" fill="none" stroke="currentColor" strokeWidth={12} />
      <rect x={652} y={258} width={84} height={72} rx={14} fill="currentColor" />
      <circle cx={694} cy={286} r={9} fill={hole} /><rect x={690} y={290} width={8} height={20} rx={3} fill={hole} />
    </g>
    case 'not_found': return <g className="dillo-float">
      <circle cx={672} cy={250} r={34} fill="none" stroke="currentColor" strokeWidth={11} />
      <path d="M696 276l30 34" stroke="currentColor" strokeWidth={14} strokeLinecap="round" />
      <text x={600} y={150} fontSize={84} fill="currentColor" style={bold}>?</text>
    </g>
    case 'rate': return <>
      <g fill="currentColor" className="dillo-float"><path d={drop(600, 110)} /><path d={drop(645, 160)} opacity={0.7} /></g>
      <path d="M60 170h70M40 210h80M70 250h50" stroke="currentColor" strokeWidth={9} strokeLinecap="round" opacity={0.6} />
    </>
    case 'server': return <g className="dillo-spin" fill="currentColor">
      <path d={star(70, 120, 22)} /><path d={star(140, 70, 16)} /><path d={star(40, 60, 12)} />
    </g>
    default: return <text x={630} y={170} fontSize={84} fill="currentColor" className="dillo-float" style={bold}>?!</text>
  }
}

/** An armadillo scene for a failure. Clicking it makes the armadillo roll up, like the real ones do. */
export function Mascot({ kind, large }: { kind: ErrorKind; large?: boolean }) {
  const s = SCENES[kind]
  const [rolls, setRolls] = useState(0)
  const hole = `var(--${s.bg})`
  return (
    <figure className="m-0 flex flex-col items-center gap-2">
      <button type="button" aria-label={tr('Погладить броненосца')} onClick={() => setRolls(rolls + 1)}
        className={'grid place-items-center rounded-2xl border-0 p-0 ' + (large ? 'w-60 h-36' : 'w-36 h-22')}
        style={{ background: hole, color: `var(--${s.fg})` }}>
        <svg viewBox="20 20 720 420" className="w-[86%] h-auto overflow-visible" aria-hidden="true">
          <ellipse cx={340} cy={392} rx={250} ry={14} fill="currentColor" opacity={0.15} />
          <Props kind={kind} hole={hole} />
          {/* key restarts the roll animation on every click */}
          <g key={rolls} className={rolls ? 'dillo-roll' : 'dillo-bob'}>
            <path d={BODY} fill="currentColor" fillRule="evenodd" transform={kind === 'server' ? 'rotate(180 340 223)' : undefined} />
          </g>
        </svg>
      </button>
      <figcaption className={'italic text-t3 ' + (large ? 'text-sm' : 'text-xs')}>{tr(s.caption)}</figcaption>
    </figure>
  )
}
