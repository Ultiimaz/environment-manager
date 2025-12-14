type MessageHandler = (data: unknown) => void
type ConnectionHandler = () => void

interface WebSocketConfig {
  url: string
  reconnectInterval?: number
  maxReconnectAttempts?: number
  onOpen?: ConnectionHandler
  onClose?: ConnectionHandler
  onError?: (error: Event) => void
  onMessage?: MessageHandler
}

export class WebSocketManager {
  private ws: WebSocket | null = null
  private config: WebSocketConfig
  private reconnectAttempts = 0
  private reconnectTimeout: number | null = null
  private intentionallyClosed = false
  private messageHandlers: Set<MessageHandler> = new Set()

  constructor(config: WebSocketConfig) {
    this.config = {
      reconnectInterval: 3000,
      maxReconnectAttempts: 10,
      ...config,
    }
  }

  connect(): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      return
    }

    this.intentionallyClosed = false

    try {
      this.ws = new WebSocket(this.config.url)

      this.ws.onopen = () => {
        this.reconnectAttempts = 0
        this.config.onOpen?.()
      }

      this.ws.onclose = () => {
        this.config.onClose?.()
        if (!this.intentionallyClosed) {
          this.scheduleReconnect()
        }
      }

      this.ws.onerror = (error) => {
        this.config.onError?.(error)
      }

      this.ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)
          this.config.onMessage?.(data)
          this.messageHandlers.forEach((handler) => handler(data))
        } catch {
          // Handle binary data or non-JSON messages
          this.config.onMessage?.(event.data)
          this.messageHandlers.forEach((handler) => handler(event.data))
        }
      }
    } catch (error) {
      console.error('WebSocket connection error:', error)
      this.scheduleReconnect()
    }
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout)
    }

    if (this.reconnectAttempts >= (this.config.maxReconnectAttempts ?? 10)) {
      console.error('Max reconnection attempts reached')
      return
    }

    this.reconnectAttempts++
    this.reconnectTimeout = window.setTimeout(() => {
      this.connect()
    }, this.config.reconnectInterval)
  }

  send(data: unknown): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      if (typeof data === 'string') {
        this.ws.send(data)
      } else {
        this.ws.send(JSON.stringify(data))
      }
    }
  }

  sendBinary(data: ArrayBuffer | Blob): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(data)
    }
  }

  addMessageHandler(handler: MessageHandler): () => void {
    this.messageHandlers.add(handler)
    return () => this.messageHandlers.delete(handler)
  }

  close(): void {
    this.intentionallyClosed = true
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout)
    }
    this.ws?.close()
    this.ws = null
  }

  get isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN
  }

  get readyState(): number | undefined {
    return this.ws?.readyState
  }
}

export function createWebSocketUrl(path: string): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.host
  return `${protocol}//${host}${path}`
}
