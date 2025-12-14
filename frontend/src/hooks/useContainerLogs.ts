import { useEffect, useCallback, useRef, useState } from 'react'
import { WebSocketManager, createWebSocketUrl } from '@/lib/websocket'

interface UseContainerLogsOptions {
  containerId: string
  enabled?: boolean
  tail?: number
  follow?: boolean
  onLog?: (line: string) => void
}

interface UseContainerLogsReturn {
  logs: string[]
  isConnected: boolean
  isFollowing: boolean
  clear: () => void
  toggleFollow: () => void
}

export function useContainerLogs({
  containerId,
  enabled = true,
  tail = 100,
  follow = true,
  onLog,
}: UseContainerLogsOptions): UseContainerLogsReturn {
  const [logs, setLogs] = useState<string[]>([])
  const [isConnected, setIsConnected] = useState(false)
  const [isFollowing, setIsFollowing] = useState(follow)
  const wsRef = useRef<WebSocketManager | null>(null)

  const handleMessage = useCallback(
    (data: unknown) => {
      if (typeof data === 'string') {
        const lines = data.split('\n').filter(Boolean)
        setLogs((prev) => {
          const newLogs = [...prev, ...lines]
          // Keep last 1000 lines
          if (newLogs.length > 1000) {
            return newLogs.slice(-1000)
          }
          return newLogs
        })
        lines.forEach((line) => onLog?.(line))
      }
    },
    [onLog]
  )

  const clear = useCallback(() => {
    setLogs([])
  }, [])

  const toggleFollow = useCallback(() => {
    setIsFollowing((prev) => !prev)
  }, [])

  useEffect(() => {
    if (!enabled || !containerId) {
      return
    }

    const params = new URLSearchParams({
      tail: tail.toString(),
      follow: isFollowing.toString(),
    })

    const ws = new WebSocketManager({
      url: createWebSocketUrl(`/ws/containers/${containerId}/logs?${params}`),
      onOpen: () => {
        setIsConnected(true)
      },
      onClose: () => {
        setIsConnected(false)
      },
      onMessage: handleMessage,
    })

    wsRef.current = ws
    ws.connect()

    return () => {
      ws.close()
      wsRef.current = null
    }
  }, [containerId, enabled, tail, isFollowing, handleMessage])

  return {
    logs,
    isConnected,
    isFollowing,
    clear,
    toggleFollow,
  }
}
