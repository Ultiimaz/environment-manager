import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { listProjects, getPostgresStatus, getRedisStatus, getSettings } from '@/services/api'
import { Section } from '@/components/ui/section'
import { Badge } from '@/components/ui/badge'

export default function Home() {
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const projects = useQuery({ queryKey: ['projects'], queryFn: listProjects })
  const postgres = useQuery({ queryKey: ['services', 'postgres'], queryFn: getPostgresStatus })
  const redis = useQuery({ queryKey: ['services', 'redis'], queryFn: getRedisStatus })

  return (
    <div className="p-6 space-y-4 max-w-5xl">
      <header>
        <h1 className="text-xl font-semibold">env-manager</h1>
        <p className="text-sm text-muted-foreground">
          {settings.data ? `version ${settings.data.version}` : 'loading…'}
          {' · '}
          <Link to="/topology" className="hover:text-foreground transition-colors">
            View topology →
          </Link>
        </p>
      </header>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <Section
          title={
            <Link to="/services/paas-postgres" className="hover:text-primary transition-colors">
              paas-postgres
            </Link>
          }
          action={
            postgres.data && (
              <Badge variant={postgres.data.running ? 'success' : 'failed'}>
                {postgres.data.running ? 'running' : 'stopped'}
              </Badge>
            )
          }
        >
          <div className="text-xs text-muted-foreground font-mono">
            {postgres.data?.image || 'postgres:16'}
          </div>
        </Section>

        <Section
          title={
            <Link to="/services/paas-redis" className="hover:text-primary transition-colors">
              paas-redis
            </Link>
          }
          action={
            redis.data && (
              <Badge variant={redis.data.running ? 'success' : 'failed'}>
                {redis.data.running ? 'running' : 'stopped'}
              </Badge>
            )
          }
        >
          <div className="text-xs text-muted-foreground font-mono">
            {redis.data?.image || 'redis:7'}
          </div>
        </Section>
      </div>

      <Section
        title={`Projects (${projects.data?.length ?? 0})`}
        action={
          <Link to="/projects" className="text-xs text-muted-foreground hover:text-foreground transition-colors">
            View all →
          </Link>
        }
      >
        {projects.isLoading && <div className="text-xs text-muted-foreground">loading…</div>}
        {projects.data && projects.data.length === 0 && (
          <div className="text-xs text-muted-foreground">
            No projects yet. <Link to="/projects" className="underline">Onboard one</Link>.
          </div>
        )}
        {projects.data && projects.data.length > 0 && (
          <ul className="divide-y divide-border -mx-4 -mb-1">
            {projects.data.map((p) => (
              <li key={p.id} className="flex items-center justify-between px-4 py-2">
                <Link to={`/projects/${p.id}`} className="text-sm font-medium hover:text-primary transition-colors">
                  {p.name}
                </Link>
                <span className="text-xs text-muted-foreground font-mono">{p.default_branch}</span>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </div>
  )
}
