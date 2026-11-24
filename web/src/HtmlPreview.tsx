import { useEffect, useMemo, useRef } from 'react'

const policy = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; media-src data: blob:; font-src data:; frame-src about:; form-action 'none'; base-uri 'none'; object-src 'none'; worker-src 'none'; connect-src 'none'"

// Report the content height, which can be smaller than the frame after a section collapses.
const measure = `<script>(() => {
  const body = document.body
  if (!body) return
  function report() {
    const root = document.documentElement
    const bottom = body.getBoundingClientRect().bottom + scrollY + (parseFloat(getComputedStyle(body).marginBottom) || 0)
    const overflow = root.scrollHeight > root.clientHeight ? root.scrollHeight : 0
    parent.postMessage({ docketContentHeight: Math.ceil(Math.max(bottom, overflow)) }, '*')
  }
  const observer = new ResizeObserver(report)
  observer.observe(body)
  observer.observe(document.documentElement)
  addEventListener('load', report)
  report()
})()</script>`

// The outer frame installs policy before parsing the document, and only relays its child's height.
function previewHTML(html: string) {
  const attr = (html + measure).replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="${policy}"><meta name="referrer" content="no-referrer"><style>html,body,iframe{width:100%;height:100%;margin:0;border:0;display:block;background:white}</style></head><body><script>
    addEventListener('message', e => {
      const height = e.data?.docketContentHeight
      if (e.source === document.querySelector('iframe')?.contentWindow && Number.isFinite(height)) {
        parent.postMessage({ docketContentHeight: height }, '*')
      }
    })
  </script><iframe title="HTML" sandbox="allow-scripts" referrerpolicy="no-referrer" srcdoc="${attr}"></iframe></body></html>`
}

export function HtmlPreview({ title, html }: { title: string; html: string }) {
  const frame = useRef<HTMLIFrameElement>(null)
  const preview = useMemo(() => previewHTML(html), [html])
  useEffect(() => {
    function resize(event: MessageEvent) {
      const el = frame.current
      if (!el || event.source !== el.contentWindow) return
      const height = event.data?.docketContentHeight
      if (Number.isFinite(height) && height >= 0) {
        // Very tall documents can still scroll inside the frame.
        el.style.height = Math.max(32, Math.min(8192, Math.ceil(height))) + 'px'
      }
    }
    window.addEventListener('message', resize)
    return () => window.removeEventListener('message', resize)
  }, [])
  return <iframe ref={frame} title={title} sandbox="allow-scripts" referrerPolicy="no-referrer" loading="lazy"
    className="block h-[min(60dvh,32rem)] w-full border-0 bg-white" srcDoc={preview} />
}
