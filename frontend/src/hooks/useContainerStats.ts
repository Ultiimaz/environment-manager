import { useEffect, useState, useCallback, useRef } from 'react'
import { WebSocketManager, createWebSocketUrl } from '@/lib/websocket'
import type { ContainerStats } from '@/types'

interface UseContainerStatsOptions {
  containerId: string
  enabled?: boolean
}

interface UseContainerStatsReturn {
  stats: ContainerStats | null
  history: ContainerStats[]
  isConnected: boolean
  error: string | null
}

export function useContainerStats({
  containerId,
  enabled = true,
}: UseContainerStatsOptions): UseContainerStatsReturn {
  const [stats, setStats] = useState<ContainerStats | null>(null)
  const [history, setHistory] = useState<ContainerStats[]>([])
  const [isConnected, setIsConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const wsRef = useRef<WebSocketManager | null>(null)

  const handleMessage = useCallback((data: unknown) => {
    const statsData = data as ContainerStats
    setStats(statsData)
    setHistory((prev) => {
      const newHistory = [...prev, statsData]
      // Keep last 60 data points (1 minute at 1s intervals)
      if (newHistory.length > 60) {
        return newHistory.slice(-60)
      }
      return newHistory
    })
  }, [])

  useEffect(() => {
    if (!enabled || !containerId) {
      return
    }

    const ws = new WebSocketManager({
      url: createWebSocketUrl(`/ws/containers/${containerId}/stats`),
      onOpen: () => {
        setIsConnected(true)
        setError(null)
      },
      onClose: () => {
        setIsConnected(false)
      },
      onError: () => {
        setError('Failed to connect to stats stream')
      },
      onMessage: handleMessage,
    })

    wsRef.current = ws
    ws.connect()

    return () => {
      ws.close()
      wsRef.current = null
    }
  }, [containerId, enabled, handleMessage])

  return { stats, history, isConnected, error }
}
