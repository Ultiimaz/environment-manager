import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { getPostgresStatus, getRedisStatus, type ServiceStatus } from '@/services/api'
import { Section } from '@/components/ui/section'
import { Badge } from '@/components/ui/badge'

interface ServiceCardProps {
  name: 'paas-postgres' | 'paas-redis'
  data?: ServiceStatus
  fallback: ServiceStatus
}

function ServiceCard({ name, data, fallback }: ServiceCardProps) {
  const s = data || fallback
  const variant = s.running ? 'success' : s.exists ? 'failed' : 'default'
  const label = s.running ? 'running' : s.exists ? 'stopped' : 'absent'
  return (
    <Section
      title={
        <Link to={`/services/${name}`} className="hover:text-primary transition-colors">
          {s.container}
        </Link>
      }
      action={<Badge variant={variant}>{label}</Badge>}
    >
      <dl className="space-y-1 text-xs">
        <div className="flex justify-between">
          <dt className="text-muted-foreground">Image</dt>
          <dd className="font-mono">{s.image}</dd>
        </div>
        <div className="flex justify-between">
          <dt className="text-muted-foreground">Network</dt>
          <dd className="font-mono">paas-net</dd>
        </div>
      </dl>
      <Link
        to={`/services/${name}`}
        className="block text-xs text-muted-foreground hover:text-foreground transition-colors pt-2 border-t border-border -mx-4 px-4 -mb-1 pb-3"
      >
        View live logs →
      </Link>
    </Section>
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
    <div className="p-6 space-y-4 max-w-5xl">
      <header>
        <h1 className="text-xl font-semibold">Services</h1>
        <p className="text-sm text-muted-foreground">
          Shared service-plane singletons. Apps that declare{' '}
          <code className="font-mono text-xs">services.postgres: true</code> or{' '}
          <code className="font-mono text-xs">services.redis: true</code> in{' '}
          <code className="font-mono text-xs">.dev/config.yaml</code> get a per-env database / ACL user provisioned inside these containers.
        </p>
      </header>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
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
