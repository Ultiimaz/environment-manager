import { useEffect, useRef, useState } from 'react'

interface Props {
  url: string
  height?: string
}

type WsStatus = 'connecting' | 'open' | 'closed' | 'error'

export function RuntimeLogViewer({ url, height = 'h-96' }: Props) {
  const [log, setLog] = useState('')
  const [status, setStatus] = useState<WsStatus>('connecting')
  const [errMsg, setErrMsg] = useState<string | null>(null)
  const preRef = useRef<HTMLPreElement>(null)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    setLog('')
    setErrMsg(null)
    setStatus('connecting')
    const ws = new WebSocket(url)
    wsRef.current = ws
    ws.onopen = () => setStatus('open')
    ws.onmessage = (ev) => {
      const text = typeof ev.data === 'string' ? ev.data : ''
      // Server sometimes sends {"error":"..."} JSON before closing.
      if (text.startsWith('{"error":')) {
        try {
          const parsed = JSON.parse(text) as { error?: string }
          if (parsed.error) {
            setErrMsg(parsed.error)
            return
          }
        } catch {
          // fall through
        }
      }
      setLog((prev) => prev + text)
    }
    ws.onerror = () => setStatus('error')
    ws.onclose = () => setStatus((s) => (s === 'error' ? s : 'closed'))
    return () => {
      ws.close()
    }
  }, [url])

  useEffect(() => {
    if (preRef.current) {
      preRef.current.scrollTop = preRef.current.scrollHeight
    }
  }, [log])

  const statusColor: Record<WsStatus, string> = {
    connecting: 'text-amber-400',
    open: 'text-emerald-400',
    closed: 'text-muted-foreground',
    error: 'text-red-400',
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">
          ws · <span className={statusColor[status]}>{status}</span>
        </span>
        {errMsg && <span className="text-red-400 font-mono">{errMsg}</span>}
      </div>
      <pre
        ref={preRef}
        className={`bg-background border border-border text-xs font-mono p-3 rounded ${height} overflow-auto whitespace-pre-wrap`}
      >
        {log || (status === 'connecting' ? 'connecting…' : status === 'closed' ? '(stream closed)' : status === 'error' ? '(stream error)' : '(waiting for output)')}
      </pre>
    </div>
  )
}
