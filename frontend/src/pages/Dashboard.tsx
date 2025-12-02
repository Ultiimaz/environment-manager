import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Box, HardDrive, Layers } from 'lucide-react'
import { getContainers, getVolumes, getComposeProjects } from '../services/api'

export default function Dashboard() {
  const { data: containers = [] } = useQuery({
    queryKey: ['containers'],
    queryFn: getContainers,
    refetchInterval: 5000,
  })

  const { data: volumes = [] } = useQuery({
    queryKey: ['volumes'],
    queryFn: getVolumes,
  })

  const { data: projects = [] } = useQuery({
    queryKey: ['composeProjects'],
    queryFn: getComposeProjects,
  })

  const runningContainers = containers.filter(c => c.state === 'running')
  const stoppedContainers = containers.filter(c => c.state !== 'running')

  return (
    <div className="p-6">
      <h1 className="text-2xl font-bold text-white mb-6">Dashboard</h1>

      {/* Stats */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <StatCard
          icon={Box}
          label="Running Containers"
          value={runningContainers.length}
          total={containers.length}
          color="green"
          to="/containers"
        />
        <StatCard
          icon={Box}
          label="Stopped Containers"
          value={stoppedContainers.length}
          total={containers.length}
          color="red"
          to="/containers"
        />
        <StatCard
          icon={HardDrive}
          label="Volumes"
          value={volumes.length}
          color="blue"
          to="/volumes"
        />
        <StatCard
          icon={Layers}
          label="Compose Projects"
          value={projects.length}
          color="purple"
          to="/compose"
        />
      </div>

      {/* Recent Containers */}
      <div className="bg-gray-800 rounded-lg p-6">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">Containers</h2>
          <Link to="/containers" className="text-blue-400 hover:text-blue-300 text-sm">
            View all →
          </Link>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="text-left text-gray-400 text-sm">
                <th className="pb-3">Name</th>
                <th className="pb-3">Image</th>
                <th className="pb-3">Status</th>
                <th className="pb-3">Subdomain</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-700">
              {containers.slice(0, 5).map(container => (
                <tr key={container.id} className="text-gray-300">
                  <td className="py-3">
                    <Link
                      to={`/containers/${container.id}`}
                      className="text-blue-400 hover:text-blue-300"
                    >
                      {container.name}
                    </Link>
                  </td>
                  <td className="py-3 text-gray-400">{container.image}</td>
                  <td className="py-3">
                    <StatusBadge status={container.state} />
                  </td>
                  <td className="py-3">
                    {container.subdomain && (
                      <a
                        href={`http://${container.subdomain}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-blue-400 hover:text-blue-300"
                      >
                        {container.subdomain}
                      </a>
                    )}
                  </td>
                </tr>
              ))}
              {containers.length === 0 && (
                <tr>
                  <td colSpan={4} className="py-8 text-center text-gray-500">
                    No containers found
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

interface StatCardProps {
  icon: React.ComponentType<{ size?: number; className?: string }>
  label: string
  value: number
  total?: number
  color: 'green' | 'red' | 'blue' | 'purple'
  to: string
}

function StatCard({ icon: Icon, label, value, total, color, to }: StatCardProps) {
  const colorClasses = {
    green: 'bg-green-500/10 text-green-400',
    red: 'bg-red-500/10 text-red-400',
    blue: 'bg-blue-500/10 text-blue-400',
    purple: 'bg-purple-500/10 text-purple-400',
  }

  return (
    <Link
      to={to}
      className="bg-gray-800 rounded-lg p-4 hover:bg-gray-750 transition-colors"
    >
      <div className="flex items-center gap-3">
        <div className={`p-2 rounded-lg ${colorClasses[color]}`}>
          <Icon size={24} />
        </div>
        <div>
          <div className="text-2xl font-bold text-white">
            {value}
            {total !== undefined && (
              <span className="text-gray-500 text-lg">/{total}</span>
            )}
          </div>
          <div className="text-sm text-gray-400">{label}</div>
        </div>
      </div>
    </Link>
  )
}

function StatusBadge({ status }: { status: string }) {
  const getStatusStyle = (status: string) => {
    switch (status) {
      case 'running':
        return 'bg-green-500/20 text-green-400'
      case 'exited':
        return 'bg-red-500/20 text-red-400'
      case 'paused':
        return 'bg-yellow-500/20 text-yellow-400'
      default:
        return 'bg-gray-500/20 text-gray-400'
    }
  }

  return (
    <span className={`px-2 py-1 rounded text-xs font-medium ${getStatusStyle(status)}`}>
      {status}
    </span>
  )
}
