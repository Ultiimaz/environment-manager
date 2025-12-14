import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { Globe, Server, Shield, Save, ExternalLink } from 'lucide-react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { getNetworkConfig, getNetworkStatus, updateNetworkConfig } from '../services/api'
import type { NetworkConfig } from '../types'

export default function Network() {
  const queryClient = useQueryClient()
  const [baseDomain, setBaseDomain] = useState('')
  const [upstreamDns, setUpstreamDns] = useState('')
  const [hasChanges, setHasChanges] = useState(false)

  const { data: config, isLoading: configLoading } = useQuery<NetworkConfig>({
    queryKey: ['networkConfig'],
    queryFn: getNetworkConfig,
  })

  useEffect(() => {
    if (config) {
      setBaseDomain(config.base_domain)
      setUpstreamDns(config.coredns.upstream_dns)
    }
  }, [config])

  const { data: status } = useQuery({
    queryKey: ['networkStatus'],
    queryFn: getNetworkStatus,
    refetchInterval: 10000,
  })

  const updateMutation = useMutation({
    mutationFn: () =>
      updateNetworkConfig({
        base_domain: baseDomain,
        coredns: { upstream_dns: upstreamDns },
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['networkConfig'] })
      queryClient.invalidateQueries({ queryKey: ['networkStatus'] })
      setHasChanges(false)
    },
  })

  const handleChange = (setter: (value: string) => void) => (e: React.ChangeEvent<HTMLInputElement>) => {
    setter(e.target.value)
    setHasChanges(true)
  }

  const handleSave = () => {
    updateMutation.mutate()
  }

  if (configLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-12 w-64" />
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {[...Array(3)].map((_, i) => (
            <Skeleton key={i} className="h-32" />
          ))}
        </div>
        <Skeleton className="h-96" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold">Network Configuration</h1>
          <p className="text-muted-foreground">Manage Traefik, CoreDNS, and network settings</p>
        </div>
        {hasChanges && (
          <Button onClick={handleSave} disabled={updateMutation.isPending}>
            <Save className="h-4 w-4 mr-2" />
            {updateMutation.isPending ? 'Saving...' : 'Save Changes'}
          </Button>
        )}
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Traefik</CardTitle>
            <Globe className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2 mb-2">
              <StatusIndicator status={status?.traefik_status || 'unknown'} />
              <span className="capitalize">{status?.traefik_status || 'unknown'}</span>
            </div>
            {status?.traefik_url && (
              <a
                href={status.traefik_url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-sm text-primary hover:underline flex items-center gap-1"
              >
                {status.traefik_url}
                <ExternalLink className="h-3 w-3" />
              </a>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">CoreDNS</CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2 mb-2">
              <StatusIndicator status={status?.coredns_status || 'unknown'} />
              <span className="capitalize">{status?.coredns_status || 'unknown'}</span>
            </div>
            <p className="text-sm text-muted-foreground">
              Resolves *.{baseDomain || 'local'}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Network</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2 mb-2">
              <StatusIndicator status="active" />
              <span>Active</span>
            </div>
            <p className="text-sm text-muted-foreground font-mono">
              {config?.subnet || '10.0.0.0/16'}
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Configuration</CardTitle>
          <CardDescription>Core network and DNS settings</CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <label className="text-sm font-medium">Base Domain</label>
              <Input
                value={baseDomain}
                onChange={handleChange(setBaseDomain)}
                placeholder="example.local"
              />
              <p className="text-xs text-muted-foreground">
                Containers will be accessible at [name].{baseDomain || 'example.local'}
              </p>
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">Upstream DNS</label>
              <Input
                value={upstreamDns}
                onChange={handleChange(setUpstreamDns)}
                placeholder="8.8.8.8"
              />
              <p className="text-xs text-muted-foreground">
                DNS server for external domain resolution
              </p>
            </div>
          </div>

          <Separator />

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <label className="text-sm font-medium">Network Name</label>
              <Input value={config?.network_name || ''} disabled />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">Subnet</label>
              <Input value={config?.subnet || ''} disabled />
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Traefik Settings</CardTitle>
          <CardDescription>Reverse proxy configuration</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Dashboard</p>
              <p className="text-sm text-muted-foreground">Enable Traefik web dashboard</p>
            </div>
            <div className="flex items-center gap-2">
              <Switch checked={config?.traefik.dashboard_enabled || false} disabled />
              <Badge variant={config?.traefik.dashboard_enabled ? 'success' : 'secondary'}>
                {config?.traefik.dashboard_enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </div>
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">HTTPS</p>
              <p className="text-sm text-muted-foreground">Enable TLS/HTTPS termination</p>
            </div>
            <div className="flex items-center gap-2">
              <Switch checked={config?.traefik.https_enabled || false} disabled />
              <Badge variant={config?.traefik.https_enabled ? 'success' : 'secondary'}>
                {config?.traefik.https_enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function StatusIndicator({ status }: { status: string }) {
  const getStatusColor = (status: string) => {
    switch (status) {
      case 'running':
      case 'active':
        return 'bg-green-500'
      case 'stopped':
      case 'exited':
        return 'bg-red-500'
      case 'not_found':
        return 'bg-muted-foreground'
      default:
        return 'bg-yellow-500'
    }
  }

  return <span className={`w-2 h-2 rounded-full ${getStatusColor(status)}`} />
}
