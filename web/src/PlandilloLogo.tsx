export function PlandilloLogo({ compact = false }: { compact?: boolean }) {
  if (compact) return <img src="/plandillo-icon.svg" width={32} height={32} alt="Plandillo" className="size-8 shrink-0" />
  return (
    <span className="plandillo-wordmark inline-block w-36 max-w-full">
      <img src="/plandillo.svg" width={733} height={128} alt="Plandillo" />
      <img src="/plandillo-inverse.svg" width={733} height={128} alt="Plandillo" />
    </span>
  )
}
