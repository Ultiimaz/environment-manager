import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Play, Square, RefreshCw, Trash2, ExternalLink, Terminal, Activity, FileText, Info, Cpu, MemoryStick, Network } from 'lucide-react'
import { Terminal as XTerm } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  getContainer,
  startContainer,
  stopContainer,
  restartContainer,
  deleteContainer,
} from '../services/api'
import { useContainerStats } from '@/hooks/useContainerStats'
import { useContainerLogs } from '@/hooks/useContainerLogs'
import { useContainerShell } from '@/hooks/useContainerShell'
import { formatBytes } from '@/lib/utils'

export default function ContainerDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState('logs')
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)

  const logsTerminalRef = useRef<HTMLDivElement>(null)
  const logsTerminalInstance = useRef<XTerm | null>(null)
  const execTerminalRef = useRef<HTMLDivElement>(null)
  const execTerminalInstance = useRef<XTerm | null>(null)
  const execFitAddon = useRef<FitAddon | null>(null)

  const { data: container, isLoading } = useQuery({
    queryKey: ['container', id],
    queryFn: () => getContainer(id!),
    enabled: !!id,
    refetchInterval: 5000,
  })

  const { stats, history: statsHistory, isConnected: statsConnected } = useContainerStats({
    containerId: id!,
    enabled: !!id && container?.state === 'running',
  })

  const { logs, isConnected: logsConnected, clear: clearLogs } = useContainerLogs({
    containerId: id!,
    enabled: !!id && activeTab === 'logs',
  })

  const {
    isConnected: shellConnected,
    sendInput,
    resize: resizeShell,
    connect: connectShell,
    disconnect: disconnectShell,
  } = useContainerShell({
    containerId: id!,
    enabled: false,
    onOutput: (data) => {
      execTerminalInstance.current?.write(data)
    },
  })

  // Initialize logs terminal
  useEffect(() => {
    if (!logsTerminalRef.current || logsTerminalInstance.current) return

    const terminal = new XTerm({
      theme: {
        background: '#09090b',
        foreground: '#fafafa',
        cursor: '#fafafa',
      },
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Monaco, Consolas, monospace',
      cursorBlink: false,
      disableStdin: true,
      scrollback: 1000,
    })

    const fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(logsTerminalRef.current)
    fitAddon.fit()

    logsTerminalInstance.current = terminal

    const resizeObserver = new ResizeObserver(() => fitAddon.fit())
    resizeObserver.observe(logsTerminalRef.current)

    return () => {
      resizeObserver.disconnect()
      terminal.dispose()
      logsTerminalInstance.current = null
    }
  }, [])

  // Write logs to terminal
  useEffect(() => {
    if (logsTerminalInstance.current && logs.length > 0) {
      const lastLog = logs[logs.length - 1]
      logsTerminalInstance.current.writeln(lastLog)
    }
  }, [logs])

  // Initialize exec terminal
  useEffect(() => {
    if (activeTab !== 'exec' || !execTerminalRef.current || execTerminalInstance.current) return

    const terminal = new XTerm({
      theme: {
        background: '#09090b',
        foreground: '#fafafa',
        cursor: '#fafafa',
      },
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Monaco, Consolas, monospace',
      cursorBlink: true,
    })

    const fitAddon = new FitAddon()
    execFitAddon.current = fitAddon
    terminal.loadAddon(fitAddon)
    terminal.open(execTerminalRef.current)
    fitAddon.fit()

    terminal.onData((data) => {
      sendInput(data)
    })

    terminal.onResize(({ rows, cols }) => {
      resizeShell(rows, cols)
    })

    execTerminalInstance.current = terminal

    const resizeObserver = new ResizeObserver(() => {
      fitAddon.fit()
    })
    resizeObserver.observe(execTerminalRef.current)

    // Connect to shell
    connectShell()

    return () => {
      resizeObserver.disconnect()
      terminal.dispose()
      execTerminalInstance.current = null
      execFitAddon.current = null
      disconnectShell()
    }
  }, [activeTab, sendInput, resizeShell, connectShell, disconnectShell])

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
    deleteMutation.mutate(id!)
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-12 w-96" />
        <div className="grid gap-4 md:grid-cols-4">
          {[...Array(4)].map((_, i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
        <Skeleton className="h-96" />
      </div>
    )
  }

  if (!container) {
    return (
      <div className="flex flex-col items-center justify-center h-96 gap-4">
        <h2 className="text-xl font-semibold">Container not found</h2>
        <Button variant="outline" onClick={() => navigate('/containers')}>
          <ArrowLeft className="h-4 w-4 mr-2" />
          Back to containers
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-4">
          <Button variant="ghost" size="icon" onClick={() => navigate('/containers')}>
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-bold">{container.name}</h1>
              <StatusBadge status={container.state} />
            </div>
            <p className="text-muted-foreground font-mono text-sm">{container.image}</p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          {container.subdomain && (
            <Button variant="outline" asChild>
              <a href={`http://${container.subdomain}`} target="_blank" rel="noopener noreferrer">
                <ExternalLink className="h-4 w-4 mr-2" />
                Open
              </a>
            </Button>
          )}
          {container.state === 'running' ? (
            <Button
              variant="outline"
              onClick={() => stopMutation.mutate(id!)}
              disabled={stopMutation.isPending}
            >
              <Square className="h-4 w-4 mr-2" />
              Stop
            </Button>
          ) : (
            <Button
              variant="outline"
              onClick={() => startMutation.mutate(id!)}
              disabled={startMutation.isPending}
            >
              <Play className="h-4 w-4 mr-2" />
              Start
            </Button>
          )}
          <Button
            variant="outline"
            onClick={() => restartMutation.mutate(id!)}
            disabled={restartMutation.isPending}
          >
            <RefreshCw className="h-4 w-4 mr-2" />
            Restart
          </Button>
          <Button variant="destructive" onClick={() => setDeleteDialogOpen(true)}>
            <Trash2 className="h-4 w-4 mr-2" />
            Delete
          </Button>
        </div>
      </div>

      {container.state === 'running' && stats && (
        <div className="grid gap-4 md:grid-cols-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between pb-2">
              <CardTitle className="text-sm font-medium">CPU Usage</CardTitle>
              <Cpu className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats.cpu_percent.toFixed(1)}%</div>
              <Progress value={Math.min(stats.cpu_percent, 100)} className="mt-2" />
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between pb-2">
              <CardTitle className="text-sm font-medium">Memory</CardTitle>
              <MemoryStick className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatBytes(stats.memory_usage)}</div>
              <Progress value={stats.memory_percent} className="mt-2" />
              <p className="text-xs text-muted-foreground mt-1">
                {stats.memory_percent.toFixed(1)}% of {formatBytes(stats.memory_limit)}
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between pb-2">
              <CardTitle className="text-sm font-medium">Network RX</CardTitle>
              <Network className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatBytes(stats.network_rx_bytes)}</div>
              <p className="text-xs text-muted-foreground mt-1">Total received</p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between pb-2">
              <CardTitle className="text-sm font-medium">Network TX</CardTitle>
              <Network className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatBytes(stats.network_tx_bytes)}</div>
              <p className="text-xs text-muted-foreground mt-1">Total sent</p>
            </CardContent>
          </Card>
        </div>
      )}

      <Tabs value={activeTab} onValueChange={setActiveTab} className="flex-1">
        <TabsList>
          <TabsTrigger value="logs" className="gap-2">
            <FileText className="h-4 w-4" />
            Logs
          </TabsTrigger>
          <TabsTrigger value="stats" className="gap-2">
            <Activity className="h-4 w-4" />
            Stats
          </TabsTrigger>
          <TabsTrigger value="exec" className="gap-2" disabled={container.state !== 'running'}>
            <Terminal className="h-4 w-4" />
            Exec
          </TabsTrigger>
          <TabsTrigger value="inspect" className="gap-2">
            <Info className="h-4 w-4" />
            Inspect
          </TabsTrigger>
        </TabsList>

        <TabsContent value="logs" className="mt-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between py-3">
              <div className="flex items-center gap-2">
                <CardTitle className="text-base">Container Logs</CardTitle>
                <div className={`w-2 h-2 rounded-full ${logsConnected ? 'bg-green-500' : 'bg-red-500'}`} />
              </div>
              <Button variant="outline" size="sm" onClick={() => {
                logsTerminalInstance.current?.clear()
                clearLogs()
              }}>
                Clear
              </Button>
            </CardHeader>
            <CardContent className="p-0">
              <div ref={logsTerminalRef} className="h-[500px] rounded-b-lg overflow-hidden" />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="stats" className="mt-4">
          <Card>
            <CardHeader>
              <div className="flex items-center gap-2">
                <CardTitle>Resource Usage</CardTitle>
                <div className={`w-2 h-2 rounded-full ${statsConnected ? 'bg-green-500' : 'bg-red-500'}`} />
              </div>
              <CardDescription>Real-time resource monitoring</CardDescription>
            </CardHeader>
            <CardContent>
              {container.state !== 'running' ? (
                <div className="text-center py-12 text-muted-foreground">
                  Container is not running. Start the container to view stats.
                </div>
              ) : statsHistory.length === 0 ? (
                <div className="text-center py-12 text-muted-foreground">
                  Collecting stats data...
                </div>
              ) : (
                <div className="space-y-6">
                  <div>
                    <h4 className="font-medium mb-2">CPU Usage History</h4>
                    <div className="h-32 flex items-end gap-0.5">
                      {statsHistory.map((s, i) => (
                        <div
                          key={i}
                          className="flex-1 bg-primary rounded-t min-w-[2px]"
                          style={{ height: `${Math.min(s.cpu_percent, 100)}%` }}
                          title={`${s.cpu_percent.toFixed(1)}%`}
                        />
                      ))}
                    </div>
                  </div>
                  <Separator />
                  <div>
                    <h4 className="font-medium mb-2">Memory Usage History</h4>
                    <div className="h-32 flex items-end gap-0.5">
                      {statsHistory.map((s, i) => (
                        <div
                          key={i}
                          className="flex-1 bg-blue-500 rounded-t min-w-[2px]"
                          style={{ height: `${s.memory_percent}%` }}
                          title={`${formatBytes(s.memory_usage)} (${s.memory_percent.toFixed(1)}%)`}
                        />
                      ))}
                    </div>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="exec" className="mt-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between py-3">
              <div className="flex items-center gap-2">
                <CardTitle className="text-base">Interactive Shell</CardTitle>
                <div className={`w-2 h-2 rounded-full ${shellConnected ? 'bg-green-500' : 'bg-red-500'}`} />
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={shellConnected ? disconnectShell : connectShell}
              >
                {shellConnected ? 'Disconnect' : 'Connect'}
              </Button>
            </CardHeader>
            <CardContent className="p-0">
              <div ref={execTerminalRef} className="h-[500px] rounded-b-lg overflow-hidden" />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="inspect" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle>Container Details</CardTitle>
              <CardDescription>Configuration and runtime information</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                <div className="grid gap-4 md:grid-cols-2">
                  <div>
                    <h4 className="text-sm font-medium text-muted-foreground">Container ID</h4>
                    <p className="font-mono text-sm">{container.id}</p>
                  </div>
                  <div>
                    <h4 className="text-sm font-medium text-muted-foreground">Image</h4>
                    <p className="font-mono text-sm">{container.image}</p>
                  </div>
                  <div>
                    <h4 className="text-sm font-medium text-muted-foreground">State</h4>
                    <p className="capitalize">{container.state}</p>
                  </div>
                  <div>
                    <h4 className="text-sm font-medium text-muted-foreground">Status</h4>
                    <p>{container.status}</p>
                  </div>
                  <div>
                    <h4 className="text-sm font-medium text-muted-foreground">Created</h4>
                    <p>{new Date(container.created_at).toLocaleString()}</p>
                  </div>
                  {container.subdomain && (
                    <div>
                      <h4 className="text-sm font-medium text-muted-foreground">Subdomain</h4>
                      <p>{container.subdomain}</p>
                    </div>
                  )}
                </div>
                <Separator />
                <div>
                  <h4 className="text-sm font-medium text-muted-foreground mb-2">Ports</h4>
                  {container.ports?.length ? (
                    <div className="flex flex-wrap gap-2">
                      {container.ports.map((port, i) => (
                        <Badge key={i} variant="secondary">{port}</Badge>
                      ))}
                    </div>
                  ) : (
                    <p className="text-muted-foreground">No ports exposed</p>
                  )}
                </div>
                <div>
                  <h4 className="text-sm font-medium text-muted-foreground mb-2">Management</h4>
                  <Badge variant={container.is_managed ? 'success' : 'secondary'}>
                    {container.is_managed ? 'Managed' : 'Unmanaged'}
                  </Badge>
                  {container.desired_state && (
                    <span className="ml-2 text-sm text-muted-foreground">
                      Desired: {container.desired_state}
                    </span>
                  )}
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Container</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete &quot;{container.name}&quot;? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const variants: Record<string, 'success' | 'destructive' | 'warning' | 'secondary'> = {
    running: 'success',
    exited: 'destructive',
    paused: 'warning',
    created: 'secondary',
    dead: 'destructive',
    restarting: 'warning',
  }

  return (
    <Badge variant={variants[status] || 'secondary'}>
      {status}
    </Badge>
  )
}
