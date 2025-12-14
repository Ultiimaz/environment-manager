import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Box, HardDrive, Layers, Cpu, MemoryStick, Network } from 'lucide-react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { getContainers, getVolumes, getComposeProjects, getAllContainerStats } from '../services/api'
import { cn } from '@/lib/utils'
import { formatBytes } from '@/lib/utils'

export default function Dashboard() {
  const { data: containersData, isLoading: containersLoading } = useQuery({
    queryKey: ['containers'],
    queryFn: getContainers,
    refetchInterval: 5000,
  })
  const containers = containersData ?? []

  const { data: volumesData, isLoading: volumesLoading } = useQuery({
    queryKey: ['volumes'],
    queryFn: getVolumes,
  })
  const volumes = volumesData ?? []

  const { data: projectsData, isLoading: projectsLoading } = useQuery({
    queryKey: ['composeProjects'],
    queryFn: getComposeProjects,
  })
  const projects = projectsData ?? []

  const { data: statsData } = useQuery({
    queryKey: ['allStats'],
    queryFn: getAllContainerStats,
    refetchInterval: 3000,
  })
  const stats = statsData ?? []

  const runningContainers = containers.filter(c => c.state === 'running')
  const stoppedContainers = containers.filter(c => c.state !== 'running')

  const totalCpu = stats.reduce((acc, s) => acc + s.cpu_percent, 0)
  const totalMemory = stats.reduce((acc, s) => acc + s.memory_usage, 0)
  const totalMemoryLimit = stats.reduce((acc, s) => acc + s.memory_limit, 0)
  const memoryPercent = totalMemoryLimit > 0 ? (totalMemory / totalMemoryLimit) * 100 : 0

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <p className="text-muted-foreground">Overview of your environment</p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <StatCard
          icon={Box}
          label="Running"
          value={runningContainers.length}
          total={containers.length}
          description="containers"
          variant="success"
          to="/containers"
          loading={containersLoading}
        />
        <StatCard
          icon={Box}
          label="Stopped"
          value={stoppedContainers.length}
          total={containers.length}
          description="containers"
          variant="destructive"
          to="/containers"
          loading={containersLoading}
        />
        <StatCard
          icon={HardDrive}
          label="Volumes"
          value={volumes.length}
          description="total"
          variant="info"
          to="/volumes"
          loading={volumesLoading}
        />
        <StatCard
          icon={Layers}
          label="Compose"
          value={projects.length}
          description="projects"
          variant="warning"
          to="/compose"
          loading={projectsLoading}
        />
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">CPU Usage</CardTitle>
            <Cpu className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalCpu.toFixed(1)}%</div>
            <Progress value={Math.min(totalCpu, 100)} className="mt-2" />
            <p className="text-xs text-muted-foreground mt-2">
              Across {runningContainers.length} running containers
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Memory Usage</CardTitle>
            <MemoryStick className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatBytes(totalMemory)}</div>
            <Progress value={memoryPercent} className="mt-2" />
            <p className="text-xs text-muted-foreground mt-2">
              {memoryPercent.toFixed(1)}% of {formatBytes(totalMemoryLimit)} limit
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Network I/O</CardTitle>
            <Network className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="space-y-1">
              <div className="flex justify-between text-sm">
                <span className="text-muted-foreground">Received</span>
                <span className="font-medium">
                  {formatBytes(stats.reduce((acc, s) => acc + s.network_rx_bytes, 0))}
                </span>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-muted-foreground">Sent</span>
                <span className="font-medium">
                  {formatBytes(stats.reduce((acc, s) => acc + s.network_tx_bytes, 0))}
                </span>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>Containers</CardTitle>
            <CardDescription>Recent container activity</CardDescription>
          </div>
          <Link to="/containers">
            <Button variant="outline" size="sm">View all</Button>
          </Link>
        </CardHeader>
        <CardContent>
          {containersLoading ? (
            <div className="space-y-2">
              {[...Array(5)].map((_, i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : containers.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              No containers found
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
                  <TableHead>Subdomain</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {containers.slice(0, 5).map(container => {
                  const containerStats = stats.find(s => s.container_id === container.id)
                  return (
                    <TableRow key={container.id}>
                      <TableCell>
                        <Link
                          to={`/containers/${container.id}`}
                          className="text-primary hover:underline font-medium"
                        >
                          {container.name}
                        </Link>
                      </TableCell>
                      <TableCell className="text-muted-foreground font-mono text-sm">
                        {container.image.split(':')[0]}
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
                        {container.subdomain && (
                          <a
                            href={`http://${container.subdomain}`}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-primary hover:underline"
                          >
                            {container.subdomain}
                          </a>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

interface StatCardProps {
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: number
  total?: number
  description: string
  variant: 'success' | 'destructive' | 'info' | 'warning'
  to: string
  loading?: boolean
}

function StatCard({ icon: Icon, label, value, total, description, variant, to, loading }: StatCardProps) {
  const variantStyles = {
    success: 'bg-success/10 text-success',
    destructive: 'bg-destructive/10 text-destructive',
    info: 'bg-info/10 text-info',
    warning: 'bg-warning/10 text-warning',
  }

  if (loading) {
    return (
      <Card>
        <CardContent className="pt-6">
          <div className="flex items-center gap-4">
            <Skeleton className="h-12 w-12 rounded-lg" />
            <div className="space-y-2">
              <Skeleton className="h-6 w-16" />
              <Skeleton className="h-4 w-24" />
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Link to={to}>
      <Card className="hover:bg-accent/50 transition-colors cursor-pointer">
        <CardContent className="pt-6">
          <div className="flex items-center gap-4">
            <div className={cn('p-3 rounded-lg', variantStyles[variant])}>
              <Icon className="h-6 w-6" />
            </div>
            <div>
              <div className="text-2xl font-bold">
                {value}
                {total !== undefined && (
                  <span className="text-muted-foreground text-lg">/{total}</span>
                )}
              </div>
              <div className="text-sm text-muted-foreground">
                {label} {description}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>
    </Link>
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
