import { useEffect, useRef, useState } from 'react'
import { buildLogWsUrl } from '@/services/api'

interface Props {
  envId: string
}

export function BuildLogViewer({ envId }: Props) {
  const [log, setLog] = useState('')
  const [status, setStatus] = useState<'connecting' | 'open' | 'closed' | 'error'>('connecting')
  const preRef = useRef<HTMLPreElement>(null)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    setLog('')
    setStatus('connecting')
    const ws = new WebSocket(buildLogWsUrl(envId))
    wsRef.current = ws
    ws.onopen = () => setStatus('open')
    ws.onmessage = ev => {
      const text = typeof ev.data === 'string' ? ev.data : ''
      setLog(prev => prev + text)
    }
    ws.onerror = () => setStatus('error')
    ws.onclose = () => setStatus(s => (s === 'error' ? s : 'closed'))
    return () => {
      ws.close()
    }
  }, [envId])

  useEffect(() => {
    if (preRef.current) {
      preRef.current.scrollTop = preRef.current.scrollHeight
    }
  }, [log])

  return (
    <div>
      <div className="text-xs text-muted-foreground mb-1">
        WebSocket: {status}
      </div>
      <pre
        ref={preRef}
        className="bg-black text-green-400 font-mono text-xs p-3 rounded h-96 overflow-auto whitespace-pre-wrap"
      >
        {log || (status === 'connecting' ? 'connecting...' : status === 'closed' ? '(no log yet — trigger a build)' : '')}
      </pre>
    </div>
  )
}
