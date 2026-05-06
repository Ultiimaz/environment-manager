import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { ChevronRight } from 'lucide-react'
import { getPostgresStatus, getRedisStatus, type ServiceStatus } from '@/services/api'
import { cn } from '@/lib/utils'

// Stitch design language applied to Services list. Two singleton service
// cards, each linking through to the detail page.

const STATUS_TOKENS = {
  running: { dot: 'bg-primary', text: 'text-primary', bg: 'bg-primary/10', border: 'border-primary/30', pulse: true },
  stopped: { dot: 'bg-destructive', text: 'text-destructive', bg: 'bg-destructive/10', border: 'border-destructive/30', pulse: false },
  absent: { dot: 'bg-muted-foreground', text: 'text-muted-foreground', bg: 'bg-secondary', border: 'border-border', pulse: false },
} as const

function StatusPill({ status }: { status: keyof typeof STATUS_TOKENS }) {
  const tok = STATUS_TOKENS[status]
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10px] uppercase tracking-wider font-medium',
        tok.border,
        tok.bg,
        tok.text
      )}
    >
      <span className={cn('h-1.5 w-1.5 rounded-full', tok.dot, tok.pulse && 'animate-pulse')} />
      {status}
    </span>
  )
}

interface ServiceCardProps {
  name: 'paas-postgres' | 'paas-redis'
  data?: ServiceStatus
  fallback: ServiceStatus
}

function ServiceCard({ name, data, fallback }: ServiceCardProps) {
  const s = data || fallback
  const status: 'running' | 'stopped' | 'absent' = s.running
    ? 'running'
    : s.exists
      ? 'stopped'
      : 'absent'

  return (
    <Link
      to={`/services/${name}`}
      className="block rounded-lg border border-border bg-card p-4 hover:bg-secondary/40 transition-colors group"
    >
      <div className="flex items-center justify-between mb-3">
        <span className="font-mono text-[14px] font-semibold group-hover:text-primary transition-colors">
          {s.container}
        </span>
        <StatusPill status={status} />
      </div>
      <dl className="space-y-2 text-[12px] mb-3">
        <Row label="image" value={s.image} />
        <Row label="network" value="paas-net" />
      </dl>
      <div className="pt-3 border-t border-border flex items-center justify-between text-[11px] text-muted-foreground group-hover:text-foreground transition-colors">
        <span>View live logs</span>
        <ChevronRight className="h-3.5 w-3.5" />
      </div>
    </Link>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between items-center gap-3">
      <dt className="font-mono text-[11px] text-muted-foreground uppercase tracking-wider">
        {label}
      </dt>
      <dd className="font-mono text-[12px] truncate">{value}</dd>
    </div>
  )
}

export default function Services() {
  const postgres = useQuery({
    queryKey: ['services', 'postgres'],
    queryFn: getPostgresStatus,
    refetchInterval: 15000,
  })
  const redis = useQuery({
    queryKey: ['services', 'redis'],
    queryFn: getRedisStatus,
    refetchInterval: 15000,
  })

  return (
    <div className="px-6 py-6 space-y-6 max-w-[1400px] mx-auto">
      <header>
        <h1 className="text-[28px] font-semibold tracking-tight leading-none">Services</h1>
        <p className="mt-1.5 text-sm text-muted-foreground">
          Shared service-plane singletons. Apps that declare{' '}
          <code className="font-mono text-xs text-foreground">services.postgres: true</code> or{' '}
          <code className="font-mono text-xs text-foreground">services.redis: true</code> in{' '}
          <code className="font-mono text-xs text-foreground">.dev/config.yaml</code> get a per-env
          database / ACL user provisioned inside these containers.
        </p>
      </header>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <ServiceCard
          name="paas-postgres"
          data={postgres.data}
          fallback={{ container: 'paas-postgres', image: 'postgres:16', running: false, exists: false }}
        />
        <ServiceCard
          name="paas-redis"
          data={redis.data}
          fallback={{ container: 'paas-redis', image: 'redis:7', running: false, exists: false }}
        />
      </div>
    </div>
  )
}
