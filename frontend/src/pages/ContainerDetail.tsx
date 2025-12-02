import { useEffect, useRef } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Play, Square, RefreshCw, Trash2, ExternalLink } from 'lucide-react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import {
  getContainer,
  startContainer,
  stopContainer,
  restartContainer,
  deleteContainer
} from '../services/api'
import { useWebSocket } from '../hooks/useWebSocket'

export default function ContainerDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const terminalRef = useRef<HTMLDivElement>(null)
  const terminalInstanceRef = useRef<Terminal | null>(null)

  const { data: container, isLoading } = useQuery({
    queryKey: ['container', id],
    queryFn: () => getContainer(id!),
    enabled: !!id,
    refetchInterval: 5000,
  })

  // WebSocket for logs
  const { isConnected } = useWebSocket(
    id ? `/ws/containers/${id}/logs?tail=200&follow=true` : null,
    {
      onMessage: (data) => {
        if (terminalInstanceRef.current) {
          // Skip Docker log header (8 bytes)
          const message = data.length > 8 ? data.slice(8) : data
          terminalInstanceRef.current.writeln(message)
        }
      },
    }
  )

  // Initialize terminal
  useEffect(() => {
    if (!terminalRef.current || terminalInstanceRef.current) return

    const terminal = new Terminal({
      theme: {
        background: '#1f2937',
        foreground: '#e5e7eb',
      },
      fontSize: 13,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      cursorBlink: false,
      disableStdin: true,
    })

    const fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(terminalRef.current)
    fitAddon.fit()

    terminalInstanceRef.current = terminal

    // Handle resize
    const resizeObserver = new ResizeObserver(() => {
      fitAddon.fit()
    })
    resizeObserver.observe(terminalRef.current)

    return () => {
      resizeObserver.disconnect()
      terminal.dispose()
      terminalInstanceRef.current = null
    }
  }, [])

  const startMutation = useMutation({
    mutationFn: startContainer,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['container', id] }),
  })

  const stopMutation = useMutation({
    mutationFn: stopContainer,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['container', id] }),
  })

  const restartMutation = useMutation({
    mutationFn: restartContainer,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['container', id] }),
  })

  const deleteMutation = useMutation({
    mutationFn: deleteContainer,
    onSuccess: () => navigate('/containers'),
  })

  const handleDelete = () => {
    if (confirm(`Delete container "${container?.name}"?`)) {
      deleteMutation.mutate(id!)
    }
  }

  if (isLoading) {
    return <div className="p-6 text-gray-400">Loading...</div>
  }

  if (!container) {
    return <div className="p-6 text-gray-400">Container not found</div>
  }

  return (
    <div className="p-6 h-full flex flex-col">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-4">
          <button
            onClick={() => navigate('/containers')}
            className="p-2 text-gray-400 hover:text-white hover:bg-gray-800 rounded-lg"
          >
            <ArrowLeft size={20} />
          </button>
          <div>
            <h1 className="text-2xl font-bold text-white">{container.name}</h1>
            <p className="text-gray-400 text-sm">{container.image}</p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          {container.state === 'running' ? (
            <button
              onClick={() => stopMutation.mutate(id!)}
              className="flex items-center gap-2 px-3 py-2 bg-red-600/20 text-red-400 rounded-lg hover:bg-red-600/30"
            >
              <Square size={16} />
              Stop
            </button>
          ) : (
            <button
              onClick={() => startMutation.mutate(id!)}
              className="flex items-center gap-2 px-3 py-2 bg-green-600/20 text-green-400 rounded-lg hover:bg-green-600/30"
            >
              <Play size={16} />
              Start
            </button>
          )}
          <button
            onClick={() => restartMutation.mutate(id!)}
            className="flex items-center gap-2 px-3 py-2 bg-blue-600/20 text-blue-400 rounded-lg hover:bg-blue-600/30"
          >
            <RefreshCw size={16} />
            Restart
          </button>
          <button
            onClick={handleDelete}
            className="flex items-center gap-2 px-3 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600"
          >
            <Trash2 size={16} />
            Delete
          </button>
        </div>
      </div>

      {/* Info Cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
        <div className="bg-gray-800 rounded-lg p-4">
          <div className="text-sm text-gray-400 mb-1">Status</div>
          <div className="flex items-center gap-2">
            <span className={`w-2 h-2 rounded-full ${
              container.state === 'running' ? 'bg-green-500' : 'bg-red-500'
            }`} />
            <span className="text-white font-medium capitalize">{container.state}</span>
          </div>
        </div>

        <div className="bg-gray-800 rounded-lg p-4">
          <div className="text-sm text-gray-400 mb-1">Container ID</div>
          <div className="text-white font-mono text-sm">{container.id}</div>
        </div>

        {container.subdomain && (
          <div className="bg-gray-800 rounded-lg p-4">
            <div className="text-sm text-gray-400 mb-1">Subdomain</div>
            <a
              href={`http://${container.subdomain}`}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-1 text-blue-400 hover:text-blue-300"
            >
              {container.subdomain}
              <ExternalLink size={14} />
            </a>
          </div>
        )}
      </div>

      {/* Logs */}
      <div className="flex-1 bg-gray-800 rounded-lg overflow-hidden flex flex-col min-h-0">
        <div className="flex items-center justify-between px-4 py-2 border-b border-gray-700">
          <div className="flex items-center gap-2">
            <span className="text-white font-medium">Logs</span>
            <span className={`w-2 h-2 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`} />
          </div>
          <button
            onClick={() => {
              terminalInstanceRef.current?.clear()
            }}
            className="text-sm text-gray-400 hover:text-white"
          >
            Clear
          </button>
        </div>
        <div ref={terminalRef} className="flex-1 min-h-0" />
      </div>
    </div>
  )
}
