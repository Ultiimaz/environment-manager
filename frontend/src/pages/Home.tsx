import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { listProjects, getPostgresStatus, getRedisStatus, getSettings } from '@/services/api'
import { cn } from '@/lib/utils'

// Stitch Overview design (.design/stitch-v1/02-overview.html)
// Single green accent (#10B981 = primary), Geist, dense info.

function MetricTile({ label, value, hint }: { label: string; value: string | number; hint?: string }) {
  return (
    <div className="rounded-lg border border-border bg-card p-4 hover:bg-secondary/40 transition-colors">
      <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground/80">
        {label}
      </div>
      <div className="mt-2 font-mono text-2xl font-medium tabular-nums">{value}</div>
      {hint && <div className="mt-1 text-[11px] text-muted-foreground/60">{hint}</div>}
    </div>
  )
}

function StatusDot({ ok }: { ok: boolean }) {
  return (
    <span
      className={cn(
        'inline-block h-1.5 w-1.5 rounded-full',
        ok ? 'bg-primary' : 'bg-destructive'
      )}
    />
  )
}

function ServiceHealthCard({
  name,
  image,
  ok,
  href,
}: {
  name: string
  image: string
  ok: boolean
  href: string
}) {
  return (
    <Link
      to={href}
      className="block rounded-lg border border-border bg-card p-3 hover:bg-secondary/40 transition-colors"
    >
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 min-w-0">
          <StatusDot ok={ok} />
          <span className="font-mono text-[13px] truncate">{name}</span>
        </div>
        <span
          className={cn(
            'text-[10px] uppercase tracking-wider font-medium',
            ok ? 'text-primary' : 'text-destructive'
          )}
        >
          {ok ? 'running' : 'stopped'}
        </span>
      </div>
      <div className="mt-1 ml-3.5 text-[11px] text-muted-foreground/70 font-mono">{image}</div>
    </Link>
  )
}

function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="rounded border border-border bg-secondary px-1.5 py-0.5 text-[10px] font-mono">
      {children}
    </kbd>
  )
}

export default function Home() {
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const projects = useQuery({ queryKey: ['projects'], queryFn: listProjects })
  const postgres = useQuery({ queryKey: ['services', 'postgres'], queryFn: getPostgresStatus })
  const redis = useQuery({ queryKey: ['services', 'redis'], queryFn: getRedisStatus })

  const projectCount = projects.data?.length ?? 0
  const serviceCount = 2 // postgres + redis (singletons)

  return (
    <div className="px-6 py-6 space-y-6 max-w-[1400px] mx-auto">
      {/* Header */}
      <header>
        <h1 className="text-[28px] font-semibold tracking-tight leading-none">Overview</h1>
        <p className="mt-1.5 text-sm text-muted-foreground">
          All projects, environments, and singleton services on this lab
          {settings.data && (
            <span className="text-muted-foreground/50"> · v{settings.data.version}</span>
          )}
        </p>
      </header>

      {/* Metric tiles */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <MetricTile label="Projects" value={projectCount} />
        <MetricTile label="Environments" value="—" hint="aggregate via API" />
        <MetricTile label="Services" value={serviceCount} />
        <MetricTile label="Builds 24h" value="—" hint="aggregate via API" />
      </div>

      {/* Two-column grid: recent activity + service health */}
      <div className="grid grid-cols-1 lg:grid-cols-5 gap-4">
        {/* Recent activity */}
        <div className="lg:col-span-3 rounded-lg border border-border bg-card overflow-hidden">
          <div className="flex items-center justify-between px-4 py-3 border-b border-border">
            <div>
              <h2 className="text-[13px] font-semibold uppercase tracking-wider">
                Recent activity
              </h2>
              <p className="text-[11px] text-muted-foreground/70 mt-0.5">
                Build, deploy, and compose-up events across the lab
              </p>
            </div>
            <Link
              to="/builds"
              className="text-[11px] uppercase tracking-wider text-muted-foreground hover:text-foreground"
            >
              View all →
            </Link>
          </div>
          <div className="divide-y divide-border">
            {/* Until a real activity feed endpoint exists, render the project list as a stand-in */}
            {projects.isLoading && (
              <div className="px-4 py-6 text-[12px] text-muted-foreground">loading…</div>
            )}
            {projects.data && projects.data.length === 0 && (
              <div className="px-4 py-6 text-[12px] text-muted-foreground">
                No projects yet.{' '}
                <Link to="/projects" className="text-primary hover:underline">
                  Onboard one →
                </Link>
              </div>
            )}
            {projects.data?.slice(0, 6).map((p) => (
              <Link
                key={p.id}
                to={`/projects/${p.id}`}
                className="grid grid-cols-[1fr_auto_auto] items-center gap-4 px-4 py-2.5 hover:bg-secondary/40 transition-colors text-[12px]"
              >
                <span className="font-medium truncate">{p.name}</span>
                <span className="font-mono text-muted-foreground tabular-nums">
                  {p.default_branch}
                </span>
                <span
                  className={cn(
                    'inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10px] uppercase tracking-wider font-medium',
                    p.status === 'running' || p.status === 'active'
                      ? 'border-primary/30 bg-primary/10 text-primary'
                      : 'border-border bg-secondary text-muted-foreground'
                  )}
                >
                  <StatusDot ok={p.status === 'running' || p.status === 'active'} />
                  {p.status || 'idle'}
                </span>
              </Link>
            ))}
          </div>
        </div>

        {/* Service health */}
        <div className="lg:col-span-2 space-y-3">
          <div>
            <h2 className="text-[13px] font-semibold uppercase tracking-wider">
              Service health
            </h2>
            <p className="text-[11px] text-muted-foreground/70 mt-0.5">
              Singleton services consumed by environments
            </p>
          </div>
          <div className="space-y-2">
            <ServiceHealthCard
              name="paas-postgres"
              image={postgres.data?.image || 'postgres:16'}
              ok={!!postgres.data?.running}
              href="/services/paas-postgres"
            />
            <ServiceHealthCard
              name="paas-redis"
              image={redis.data?.image || 'redis:7'}
              ok={!!redis.data?.running}
              href="/services/paas-redis"
            />
          </div>
        </div>
      </div>

      {/* Keyboard shortcut hints */}
      <footer className="pt-4 mt-2 border-t border-border flex flex-wrap items-center justify-center gap-x-6 gap-y-2 text-[11px] text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <Kbd>⌘</Kbd>
          <Kbd>K</Kbd>
          <span>Search</span>
        </span>
        <span className="flex items-center gap-1.5">
          <Kbd>G</Kbd>
          <Kbd>O</Kbd>
          <span>Overview</span>
        </span>
        <span className="flex items-center gap-1.5">
          <Kbd>G</Kbd>
          <Kbd>P</Kbd>
          <span>Projects</span>
        </span>
        <span className="flex items-center gap-1.5">
          <Kbd>G</Kbd>
          <Kbd>S</Kbd>
          <span>Services</span>
        </span>
        <span className="flex items-center gap-1.5">
          <Kbd>G</Kbd>
          <Kbd>T</Kbd>
          <span>Topology</span>
        </span>
      </footer>
    </div>
  )
}
