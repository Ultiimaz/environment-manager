import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { listProjects, getPostgresStatus, getRedisStatus, getSettings } from '@/services/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

export default function Home() {
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const projects = useQuery({ queryKey: ['projects'], queryFn: listProjects })
  const postgres = useQuery({ queryKey: ['services', 'postgres'], queryFn: getPostgresStatus })
  const redis = useQuery({ queryKey: ['services', 'redis'], queryFn: getRedisStatus })

  return (
    <div className="p-6 space-y-6">
      <div>
        <h1 className="text-2xl font-bold">env-manager</h1>
        <p className="text-sm text-muted-foreground">
          {settings.data ? `version ${settings.data.version}` : 'loading…'}
        </p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center justify-between">
              paas-postgres
              {postgres.data && (
                <Badge variant={postgres.data.running ? 'default' : 'destructive'}>
                  {postgres.data.running ? 'running' : 'stopped'}
                </Badge>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-sm text-muted-foreground">
              {postgres.data?.image || 'postgres:16'}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center justify-between">
              paas-redis
              {redis.data && (
                <Badge variant={redis.data.running ? 'default' : 'destructive'}>
                  {redis.data.running ? 'running' : 'stopped'}
                </Badge>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-sm text-muted-foreground">
              {redis.data?.image || 'redis:7'}
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Projects ({projects.data?.length ?? 0})</CardTitle>
        </CardHeader>
        <CardContent>
          {projects.isLoading && <div className="text-sm text-muted-foreground">loading…</div>}
          {projects.data && projects.data.length === 0 && (
            <div className="text-sm text-muted-foreground">
              No projects yet. <Link to="/projects" className="underline">Onboard one</Link>.
            </div>
          )}
          {projects.data && projects.data.length > 0 && (
            <ul className="space-y-2">
              {projects.data.map((p) => (
                <li key={p.id} className="flex items-center justify-between">
                  <Link to={`/projects/${p.id}`} className="font-medium hover:underline">
                    {p.name}
                  </Link>
                  <span className="text-xs text-muted-foreground">{p.default_branch}</span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
