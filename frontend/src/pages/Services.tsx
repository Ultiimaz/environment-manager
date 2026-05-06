import { useQuery } from '@tanstack/react-query'
import { getPostgresStatus, getRedisStatus, type ServiceStatus } from '@/services/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

function ServiceCard({ data, fallback }: { data?: ServiceStatus; fallback: ServiceStatus }) {
  const s = data || fallback
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center justify-between">
          {s.container}
          <Badge variant={s.running ? 'default' : 'destructive'}>
            {s.running ? 'running' : s.exists ? 'stopped' : 'absent'}
          </Badge>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-1 text-sm">
        <div>
          <span className="text-muted-foreground">Image: </span>
          <span className="font-mono">{s.image}</span>
        </div>
        <div>
          <span className="text-muted-foreground">Network: </span>
          <span className="font-mono">paas-net</span>
        </div>
      </CardContent>
    </Card>
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
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">Services</h1>
      <p className="text-sm text-muted-foreground">
        Shared service-plane singletons used by every project that declares <code>services.postgres: true</code> or <code>services.redis: true</code> in <code>.dev/config.yaml</code>.
      </p>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <ServiceCard
          data={postgres.data}
          fallback={{ container: 'paas-postgres', image: 'postgres:16', running: false, exists: false }}
        />
        <ServiceCard
          data={redis.data}
          fallback={{ container: 'paas-redis', image: 'redis:7', running: false, exists: false }}
        />
      </div>
    </div>
  )
}
