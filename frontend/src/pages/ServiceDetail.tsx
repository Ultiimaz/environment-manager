import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import {
  getPostgresStatus,
  getRedisStatus,
  serviceRuntimeLogWsUrl,
  type ServiceStatus,
} from '@/services/api'
import { RuntimeLogViewer } from '@/components/runtime-log-viewer'
import { cn } from '@/lib/utils'

// Stitch Service Detail (.design/stitch-v1/07-service-detail.html)
// Singleton service: header with image/status pill, configuration grid,
// live runtime logs. Aspirational consumer/backup/slow-query panels omitted
// until backend supplies that data.

const KNOWN: Record<
  string,
  { fetch: () => Promise<ServiceStatus>; image: string; port: number; host: string; description: string }
> = {
  'paas-postgres': {
    fetch: getPostgresStatus,
    image: 'postgres:16',
    port: 5432,
    host: 'paas-postgres',
    description: 'Singleton Postgres consumed by all environments via DATABASE_URL.',
  },
  'paas-redis': {
    fetch: getRedisStatus,
    image: 'redis:7',
    port: 6379,
    host: 'paas-redis',
    description: 'Singleton Redis consumed by all environments via REDIS_URL.',
  },
}

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

export default function ServiceDetail() {
  const { name = '' } = useParams<{ name: string }>()
  const known = KNOWN[name]

  const { data, isLoading, error } = useQuery({
    queryKey: ['service', name],
    queryFn: () => known.fetch(),
    refetchInterval: 15000,
    enabled: !!known,
  })

  if (!known) {
    return (
      <div className="px-6 py-6 max-w-3xl">
        <Link
          to="/services"
          className="inline-flex items-center text-[12px] text-muted-foreground hover:text-foreground transition-colors mb-4"
        >
          <ArrowLeft className="h-3.5 w-3.5 mr-1" /> Services
        </Link>
        <div className="rounded-lg border border-destructive/40 bg-destructive/10 p-4 text-[12px] text-destructive">
          Unknown service: {name}
        </div>
      </div>
    )
  }

  const status: 'running' | 'stopped' | 'absent' = data?.running
    ? 'running'
    : data?.exists
      ? 'stopped'
      : 'absent'

  return (
    <div className="px-6 py-6 space-y-6 max-w-[1400px] mx-auto">
      <Link
        to="/services"
        className="inline-flex items-center text-[12px] text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft className="h-3.5 w-3.5 mr-1" /> Services
      </Link>

      {/* Header */}
      <header className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-3 flex-wrap">
            <h1 className="text-[28px] font-semibold tracking-tight leading-none truncate font-mono">
              {name}
            </h1>
            <span className="inline-flex items-center rounded-full border border-border bg-secondary px-2 py-0.5 text-[10px] font-mono text-muted-foreground">
              {data?.image || known.image}
            </span>
            {data && <StatusPill status={status} />}
          </div>
          <p className="mt-2 text-sm text-muted-foreground">{known.description}</p>
        </div>
      </header>

      {/* Container info + configuration grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div className="rounded-lg border border-border bg-card overflow-hidden">
          <div className="px-4 py-3 border-b border-border bg-background/40">
            <h2 className="text-[12px] font-semibold uppercase tracking-wider">Container</h2>
          </div>
          <div className="p-4">
            {isLoading && <div className="text-[12px] text-muted-foreground">loading…</div>}
            {error && (
              <div className="text-[12px] text-destructive">{(error as Error).message}</div>
            )}
            {data && (
              <dl className="text-[12px] space-y-0">
                <Row label="container" value={data.container} />
                <Row label="image" value={data.image || known.image} />
                <Row label="status" value={status} mono={false} />
                <Row label="exists" value={data.exists ? 'yes' : 'no'} last />
              </dl>
            )}
          </div>
        </div>

        <div className="rounded-lg border border-border bg-card overflow-hidden">
          <div className="px-4 py-3 border-b border-border bg-background/40">
            <h2 className="text-[12px] font-semibold uppercase tracking-wider">Configuration</h2>
          </div>
          <div className="p-4 grid grid-cols-2 gap-y-4 gap-x-6">
            <ConfigCell label="Host" value={known.host} />
            <ConfigCell label="Port" value={String(known.port)} />
            <ConfigCell label="Image" value={data?.image || known.image} />
            <ConfigCell label="Network" value="paas-net" />
          </div>
        </div>
      </div>

      {/* Live runtime logs */}
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        <div className="flex items-center justify-between px-4 h-10 border-b border-border bg-background/40">
          <div className="flex items-center gap-2">
            <span className="text-[12px] font-semibold uppercase tracking-wider">
              Live runtime logs
            </span>
            <span className="inline-flex items-center gap-1.5 rounded border border-primary/20 bg-primary/10 px-1.5 py-0.5 text-[10px] font-mono text-primary">
              <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse" />
              ws · open
            </span>
          </div>
        </div>
        <RuntimeLogViewer url={serviceRuntimeLogWsUrl(name)} height="h-[60vh]" />
      </div>
    </div>
  )
}

function Row({ label, value, last, mono = true }: { label: string; value: string; last?: boolean; mono?: boolean }) {
  return (
    <div
      className={cn(
        'flex justify-between items-center py-2 gap-3',
        !last && 'border-b border-border'
      )}
    >
      <dt className="font-mono text-[11px] text-muted-foreground uppercase tracking-wider">
        {label}
      </dt>
      <dd className={cn('text-[12px] truncate', mono && 'font-mono')}>{value}</dd>
    </div>
  )
}

function ConfigCell({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground/80 mb-1">
        {label}
      </div>
      <div className="font-mono text-[12px] text-foreground">{value}</div>
    </div>
  )
}
