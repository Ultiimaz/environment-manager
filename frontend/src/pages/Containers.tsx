import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Play, Square, RefreshCw, Trash2, Plus, ExternalLink, Search, Filter } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import {
  getContainers,
  startContainer,
  stopContainer,
  restartContainer,
  deleteContainer,
  getAllContainerStats,
  createContainer,
} from '../services/api'
import type { Container, ContainerConfig } from '../types'
import { formatBytes } from '@/lib/utils'

export default function Containers() {
  const [searchQuery, setSearchQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState<string>('all')
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [containerToDelete, setContainerToDelete] = useState<Container | null>(null)
  const queryClient = useQueryClient()

  const { data: containersData, isLoading } = useQuery({
    queryKey: ['containers'],
    queryFn: getContainers,
    refetchInterval: 5000,
  })
  const containers = containersData ?? []

  const { data: statsData } = useQuery({
    queryKey: ['allStats'],
    queryFn: getAllContainerStats,
    refetchInterval: 3000,
  })
  const stats = statsData ?? []

  const startMutation = useMutation({
    mutationFn: startContainer,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['containers'] }),
  })

  const stopMutation = useMutation({
    mutationFn: stopContainer,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['containers'] }),
  })

  const restartMutation = useMutation({
    mutationFn: restartContainer,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['containers'] }),
  })

  const deleteMutation = useMutation({
    mutationFn: deleteContainer,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['containers'] })
      setDeleteDialogOpen(false)
      setContainerToDelete(null)
    },
  })

  const handleDeleteClick = (container: Container) => {
    setContainerToDelete(container)
    setDeleteDialogOpen(true)
  }

  const handleConfirmDelete = () => {
    if (containerToDelete) {
      deleteMutation.mutate(containerToDelete.id)
    }
  }

  const filteredContainers = containers.filter((container) => {
    const matchesSearch = container.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
      container.image.toLowerCase().includes(searchQuery.toLowerCase())
    const matchesStatus = statusFilter === 'all' || container.state === statusFilter
    return matchesSearch && matchesStatus
  })

  const runningCount = containers.filter(c => c.state === 'running').length
  const stoppedCount = containers.filter(c => c.state !== 'running').length

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold">Containers</h1>
          <p className="text-muted-foreground">
            {runningCount} running, {stoppedCount} stopped
          </p>
        </div>
        <Button onClick={() => setCreateDialogOpen(true)}>
          <Plus className="h-4 w-4 mr-2" />
          Create Container
        </Button>
      </div>

      <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="Search containers..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-9"
          />
        </div>
        <Select value={statusFilter} onValueChange={setStatusFilter}>
          <SelectTrigger className="w-full sm:w-[180px]">
            <Filter className="h-4 w-4 mr-2" />
            <SelectValue placeholder="Filter status" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Status</SelectItem>
            <SelectItem value="running">Running</SelectItem>
            <SelectItem value="exited">Exited</SelectItem>
            <SelectItem value="paused">Paused</SelectItem>
            <SelectItem value="created">Created</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="p-4 space-y-3">
              {[...Array(5)].map((_, i) => (
                <Skeleton key={i} className="h-16 w-full" />
              ))}
            </div>
          ) : filteredContainers.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              {searchQuery || statusFilter !== 'all'
                ? 'No containers match your filters'
                : 'No containers found. Create one to get started.'}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Image</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>CPU</TableHead>
                  <TableHead>Memory</TableHead>
                  <TableHead>Ports</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredContainers.map((container) => {
                  const containerStats = stats.find(s => s.container_id === container.id)
                  return (
                    <TableRow key={container.id}>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Link
                            to={`/containers/${container.id}`}
                            className="font-medium text-primary hover:underline"
                          >
                            {container.name}
                          </Link>
                          {container.is_managed && (
                            <Badge variant="outline" className="text-xs">managed</Badge>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="font-mono text-sm text-muted-foreground">
                        {container.image.length > 40
                          ? container.image.substring(0, 40) + '...'
                          : container.image}
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={container.state} />
                      </TableCell>
                      <TableCell>
                        {containerStats ? `${containerStats.cpu_percent.toFixed(1)}%` : '-'}
                      </TableCell>
                      <TableCell>
                        {containerStats ? formatBytes(containerStats.memory_usage) : '-'}
                      </TableCell>
                      <TableCell>
                        {container.ports?.length ? (
                          <span className="text-sm text-muted-foreground">
                            {container.ports.slice(0, 2).join(', ')}
                            {container.ports.length > 2 && ` +${container.ports.length - 2}`}
                          </span>
                        ) : '-'}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          {container.subdomain && (
                            <Button variant="ghost" size="icon" asChild>
                              <a
                                href={`http://${container.subdomain}`}
                                target="_blank"
                                rel="noopener noreferrer"
                                title={container.subdomain}
                              >
                                <ExternalLink className="h-4 w-4" />
                              </a>
                            </Button>
                          )}
                          {container.state === 'running' ? (
                            <Button
                              variant="ghost"
                              size="icon"
                              onClick={() => stopMutation.mutate(container.id)}
                              disabled={stopMutation.isPending}
                              title="Stop"
                            >
                              <Square className="h-4 w-4" />
                            </Button>
                          ) : (
                            <Button
                              variant="ghost"
                              size="icon"
                              onClick={() => startMutation.mutate(container.id)}
                              disabled={startMutation.isPending}
                              title="Start"
                            >
                              <Play className="h-4 w-4" />
                            </Button>
                          )}
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button variant="ghost" size="icon">
                                <RefreshCw className="h-4 w-4" />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuLabel>Actions</DropdownMenuLabel>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem
                                onClick={() => restartMutation.mutate(container.id)}
                              >
                                <RefreshCw className="h-4 w-4 mr-2" />
                                Restart
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                className="text-destructive"
                                onClick={() => handleDeleteClick(container)}
                              >
                                <Trash2 className="h-4 w-4 mr-2" />
                                Delete
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Container</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete &quot;{containerToDelete?.name}&quot;? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleConfirmDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <CreateContainerDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
      />
    </div>
  )
}

function CreateContainerDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [name, setName] = useState('')
  const [image, setImage] = useState('')
  const [hostPort, setHostPort] = useState('')
  const [containerPort, setContainerPort] = useState('')
  const [envVars, setEnvVars] = useState('')
  const queryClient = useQueryClient()

  const createMutation = useMutation({
    mutationFn: () => {
      const config: ContainerConfig = {
        image,
        ports: hostPort && containerPort ? [{ host: parseInt(hostPort), container: parseInt(containerPort) }] : undefined,
        env: envVars ? Object.fromEntries(
          envVars.split('\n').filter(Boolean).map(line => {
            const [key, ...rest] = line.split('=')
            return [key.trim(), rest.join('=').trim()]
          })
        ) : undefined,
      }
      return createContainer(name, config)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['containers'] })
      onOpenChange(false)
      resetForm()
    },
  })

  const resetForm = () => {
    setName('')
    setImage('')
    setHostPort('')
    setContainerPort('')
    setEnvVars('')
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (name && image) {
      createMutation.mutate()
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Create Container</DialogTitle>
          <DialogDescription>
            Create a new Docker container with the specified configuration.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="name">Container Name</Label>
              <Input
                id="name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="my-container"
                required
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="image">Image</Label>
              <Input
                id="image"
                value={image}
                onChange={(e) => setImage(e.target.value)}
                placeholder="nginx:latest"
                required
              />
            </div>

            <Separator />

            <div className="space-y-2">
              <Label>Port Mapping (optional)</Label>
              <div className="flex gap-2 items-center">
                <Input
                  value={hostPort}
                  onChange={(e) => setHostPort(e.target.value)}
                  placeholder="Host port"
                  type="number"
                  className="w-32"
                />
                <span className="text-muted-foreground">:</span>
                <Input
                  value={containerPort}
                  onChange={(e) => setContainerPort(e.target.value)}
                  placeholder="Container port"
                  type="number"
                  className="w-32"
                />
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="env">Environment Variables (optional)</Label>
              <textarea
                id="env"
                value={envVars}
                onChange={(e) => setEnvVars(e.target.value)}
                placeholder="KEY=value&#10;ANOTHER_KEY=another_value"
                className="w-full h-24 px-3 py-2 rounded-md border border-input bg-transparent font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
              />
              <p className="text-xs text-muted-foreground">One per line: KEY=value</p>
            </div>
          </div>

          {createMutation.isError && (
            <p className="text-sm text-destructive mb-4">
              {(createMutation.error as Error)?.message || 'Failed to create container'}
            </p>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={createMutation.isPending || !name || !image}>
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
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
