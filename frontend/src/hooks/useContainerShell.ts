import { useEffect, useCallback, useRef, useState } from 'react'
import { WebSocketManager, createWebSocketUrl } from '@/lib/websocket'

interface ShellMessage {
  type: 'resize' | 'input'
  rows?: number
  cols?: number
  data?: string
}

interface UseContainerShellOptions {
  containerId: string
  enabled?: boolean
  onOutput?: (data: string) => void
  onConnect?: () => void
  onDisconnect?: () => void
  onError?: (error: string) => void
}

interface UseContainerShellReturn {
  isConnected: boolean
  sendInput: (data: string) => void
  resize: (rows: number, cols: number) => void
  connect: () => void
  disconnect: () => void
}

export function useContainerShell({
  containerId,
  enabled = false,
  onOutput,
  onConnect,
  onDisconnect,
  onError,
}: UseContainerShellOptions): UseContainerShellReturn {
  const [isConnected, setIsConnected] = useState(false)
  const wsRef = useRef<WebSocketManager | null>(null)

  const handleMessage = useCallback(
    (data: unknown) => {
      if (typeof data === 'string') {
        onOutput?.(data)
      } else if (data instanceof ArrayBuffer) {
        const decoder = new TextDecoder()
        onOutput?.(decoder.decode(data))
      }
    },
    [onOutput]
  )

  const connect = useCallback(() => {
    if (wsRef.current?.isConnected) {
      return
    }

    const ws = new WebSocketManager({
      url: createWebSocketUrl(`/ws/containers/${containerId}/shell`),
      reconnectInterval: 5000,
      maxReconnectAttempts: 3,
      onOpen: () => {
        setIsConnected(true)
        onConnect?.()
      },
      onClose: () => {
        setIsConnected(false)
        onDisconnect?.()
      },
      onError: () => {
        onError?.('Failed to connect to shell')
      },
      onMessage: handleMessage,
    })

    wsRef.current = ws
    ws.connect()
  }, [containerId, handleMessage, onConnect, onDisconnect, onError])

  const disconnect = useCallback(() => {
    wsRef.current?.close()
    wsRef.current = null
    setIsConnected(false)
  }, [])

  const sendInput = useCallback((data: string) => {
    if (wsRef.current?.isConnected) {
      const message: ShellMessage = { type: 'input', data }
      wsRef.current.send(message)
    }
  }, [])

  const resize = useCallback((rows: number, cols: number) => {
    if (wsRef.current?.isConnected) {
      const message: ShellMessage = { type: 'resize', rows, cols }
      wsRef.current.send(message)
    }
  }, [])

  useEffect(() => {
    if (enabled && containerId) {
      connect()
    }

    return () => {
      disconnect()
    }
  }, [enabled, containerId, connect, disconnect])

  return {
    isConnected,
    sendInput,
    resize,
    connect,
    disconnect,
  }
}
