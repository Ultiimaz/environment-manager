import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { Globe, Server, Shield, Save } from 'lucide-react'
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

  // Update local state when config loads
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
    mutationFn: () => updateNetworkConfig({
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
    return <div className="p-6 text-gray-400">Loading...</div>
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-white">Network Configuration</h1>
        {hasChanges && (
          <button
            onClick={handleSave}
            disabled={updateMutation.isPending}
            className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
          >
            <Save size={20} />
            {updateMutation.isPending ? 'Saving...' : 'Save Changes'}
          </button>
        )}
      </div>

      {/* Status Cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-8">
        <StatusCard
          icon={Globe}
          label="Traefik"
          status={status?.traefik_status || 'unknown'}
          url={status?.traefik_url}
        />
        <StatusCard
          icon={Server}
          label="CoreDNS"
          status={status?.coredns_status || 'unknown'}
        />
        <StatusCard
          icon={Shield}
          label="Network"
          status="active"
          detail={config?.subnet}
        />
      </div>

      {/* Configuration */}
      <div className="bg-gray-800 rounded-lg p-6">
        <h2 className="text-lg font-semibold text-white mb-4">Configuration</h2>

        <div className="space-y-6">
          <div>
            <label className="block text-sm text-gray-400 mb-2">Base Domain</label>
            <input
              type="text"
              value={baseDomain}
              onChange={handleChange(setBaseDomain)}
              className="w-full max-w-md px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:border-blue-500"
              placeholder="example.local"
            />
            <p className="text-sm text-gray-500 mt-1">
              Containers will be accessible at [name].{baseDomain}
            </p>
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-2">Network Name</label>
            <input
              type="text"
              value={config?.network_name || ''}
              disabled
              className="w-full max-w-md px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-gray-400"
            />
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-2">Subnet</label>
            <input
              type="text"
              value={config?.subnet || ''}
              disabled
              className="w-full max-w-md px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-gray-400"
            />
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-2">Upstream DNS</label>
            <input
              type="text"
              value={upstreamDns}
              onChange={handleChange(setUpstreamDns)}
              className="w-full max-w-md px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:border-blue-500"
              placeholder="8.8.8.8"
            />
            <p className="text-sm text-gray-500 mt-1">
              DNS server for external domain resolution
            </p>
          </div>
        </div>
      </div>

      {/* Traefik Settings */}
      <div className="bg-gray-800 rounded-lg p-6 mt-6">
        <h2 className="text-lg font-semibold text-white mb-4">Traefik Settings</h2>

        <div className="space-y-4">
          <label className="flex items-center gap-3">
            <input
              type="checkbox"
              checked={config?.traefik.dashboard_enabled || false}
              disabled
              className="w-4 h-4 rounded bg-gray-700 border-gray-600"
            />
            <span className="text-gray-300">Dashboard Enabled</span>
          </label>

          <label className="flex items-center gap-3">
            <input
              type="checkbox"
              checked={config?.traefik.https_enabled || false}
              disabled
              className="w-4 h-4 rounded bg-gray-700 border-gray-600"
            />
            <span className="text-gray-300">HTTPS Enabled</span>
          </label>
        </div>
      </div>
    </div>
  )
}

interface StatusCardProps {
  icon: React.ComponentType<{ size?: number; className?: string }>
  label: string
  status: string
  url?: string
  detail?: string
}

function StatusCard({ icon: Icon, label, status, url, detail }: StatusCardProps) {
  const getStatusColor = (status: string) => {
    switch (status) {
      case 'running':
      case 'active':
        return 'bg-green-500'
      case 'stopped':
      case 'exited':
        return 'bg-red-500'
      case 'not_found':
        return 'bg-gray-500'
      default:
        return 'bg-yellow-500'
    }
  }

  return (
    <div className="bg-gray-800 rounded-lg p-4">
      <div className="flex items-center gap-3 mb-3">
        <div className="p-2 bg-gray-700 rounded-lg text-gray-400">
          <Icon size={24} />
        </div>
        <div>
          <h3 className="text-white font-medium">{label}</h3>
          <div className="flex items-center gap-2 mt-1">
            <span className={`w-2 h-2 rounded-full ${getStatusColor(status)}`} />
            <span className="text-sm text-gray-400 capitalize">{status}</span>
          </div>
        </div>
      </div>
      {url && (
        <a
          href={url}
          target="_blank"
          rel="noopener noreferrer"
          className="text-sm text-blue-400 hover:text-blue-300"
        >
          {url}
        </a>
      )}
      {detail && (
        <p className="text-sm text-gray-400 font-mono">{detail}</p>
      )}
    </div>
  )
}
