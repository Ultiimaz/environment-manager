import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import {
  getPostgresStatus,
  getRedisStatus,
  serviceRuntimeLogWsUrl,
  type ServiceStatus,
} from '@/services/api'
import { Section } from '@/components/ui/section'
import { Badge } from '@/components/ui/badge'
import { RuntimeLogViewer } from '@/components/runtime-log-viewer'

const KNOWN: Record<string, { fetch: () => Promise<ServiceStatus>; image: string }> = {
  'paas-postgres': { fetch: getPostgresStatus, image: 'postgres:16' },
  'paas-redis': { fetch: getRedisStatus, image: 'redis:7' },
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
      <div className="p-6 space-y-4 max-w-3xl">
        <Link to="/services" className="inline-flex items-center text-xs text-muted-foreground hover:text-foreground transition-colors">
          <ArrowLeft className="h-3 w-3 mr-1" /> Services
        </Link>
        <Section className="border-red-900/60">
          <div className="text-xs text-red-400">unknown service: {name}</div>
        </Section>
      </div>
    )
  }

  const status = data
  const variant: 'success' | 'failed' | 'default' =
    status?.running ? 'success' : status?.exists ? 'failed' : 'default'
  const label = status?.running ? 'running' : status?.exists ? 'stopped' : 'absent'

  return (
    <div className="p-6 space-y-4 max-w-5xl">
      <Link to="/services" className="inline-flex items-center text-xs text-muted-foreground hover:text-foreground transition-colors">
        <ArrowLeft className="h-3 w-3 mr-1" /> Services
      </Link>

      <header className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold font-mono">{name}</h1>
          <p className="text-sm text-muted-foreground">Singleton service container on the home-lab.</p>
        </div>
        {status && <Badge variant={variant}>{label}</Badge>}
      </header>

      <Section title="Container">
        {isLoading && <div className="text-xs text-muted-foreground">loading…</div>}
        {error && <div className="text-xs text-red-400">{(error as Error).message}</div>}
        {status && (
          <dl className="text-xs space-y-2">
            <div className="flex justify-between items-center">
              <dt className="text-muted-foreground">Container</dt>
              <dd className="font-mono">{status.container}</dd>
            </div>
            <div className="flex justify-between items-center">
              <dt className="text-muted-foreground">Image</dt>
              <dd className="font-mono">{status.image || known.image}</dd>
            </div>
            <div className="flex justify-between items-center">
              <dt className="text-muted-foreground">Network</dt>
              <dd className="font-mono">paas-net</dd>
            </div>
            <div className="flex justify-between items-center">
              <dt className="text-muted-foreground">Status</dt>
              <dd><Badge variant={variant}>{label}</Badge></dd>
            </div>
          </dl>
        )}
      </Section>

      <Section title="Live runtime logs">
        <RuntimeLogViewer url={serviceRuntimeLogWsUrl(name)} height="h-[60vh]" />
      </Section>
    </div>
  )
}
