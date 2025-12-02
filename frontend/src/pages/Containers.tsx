import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Play, Square, RefreshCw, Trash2, Plus, ExternalLink } from 'lucide-react'
import {
  getContainers,
  startContainer,
  stopContainer,
  restartContainer,
  deleteContainer
} from '../services/api'
import type { Container } from '../types'
import CreateContainerModal from '../components/CreateContainerModal'

export default function Containers() {
  const [showCreateModal, setShowCreateModal] = useState(false)
  const queryClient = useQueryClient()

  const { data: containers = [], isLoading } = useQuery({
    queryKey: ['containers'],
    queryFn: getContainers,
    refetchInterval: 5000,
  })

  const startMutation = useMutation({
    mutationFn: startContainer,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['containers'] }),
  })

  const stopMutation = useMutation({
    mutationFn: stopContainer,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['containers'] }),
  })

  const restartMutation = useMutation({
    mutationFn: restartContainer,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['containers'] }),
  })

  const deleteMutation = useMutation({
    mutationFn: deleteContainer,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['containers'] }),
  })

  const handleDelete = (container: Container) => {
    if (confirm(`Delete container "${container.name}"?`)) {
      deleteMutation.mutate(container.id)
    }
  }

  if (isLoading) {
    return <div className="p-6 text-gray-400">Loading...</div>
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-white">Containers</h1>
        <button
          onClick={() => setShowCreateModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors"
        >
          <Plus size={20} />
          Create Container
        </button>
      </div>

      <div className="bg-gray-800 rounded-lg overflow-hidden">
        <table className="w-full">
          <thead className="bg-gray-750">
            <tr className="text-left text-gray-400 text-sm">
              <th className="px-4 py-3">Name</th>
              <th className="px-4 py-3">Image</th>
              <th className="px-4 py-3">Status</th>
              <th className="px-4 py-3">Ports</th>
              <th className="px-4 py-3">Subdomain</th>
              <th className="px-4 py-3">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-700">
            {containers.map(container => (
              <tr key={container.id} className="text-gray-300 hover:bg-gray-750">
                <td className="px-4 py-3">
                  <Link
                    to={`/containers/${container.id}`}
                    className="text-blue-400 hover:text-blue-300 font-medium"
                  >
                    {container.name}
                  </Link>
                  {container.is_managed && (
                    <span className="ml-2 text-xs text-green-400">managed</span>
                  )}
                </td>
                <td className="px-4 py-3 text-gray-400 font-mono text-sm">
                  {container.image}
                </td>
                <td className="px-4 py-3">
                  <StatusBadge status={container.state} />
                </td>
                <td className="px-4 py-3 text-gray-400 text-sm">
                  {container.ports?.join(', ') || '-'}
                </td>
                <td className="px-4 py-3">
                  {container.subdomain && (
                    <a
                      href={`http://${container.subdomain}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex items-center gap-1 text-blue-400 hover:text-blue-300 text-sm"
                    >
                      {container.subdomain}
                      <ExternalLink size={14} />
                    </a>
                  )}
                </td>
                <td className="px-4 py-3">
                  <div className="flex items-center gap-2">
                    {container.state === 'running' ? (
                      <button
                        onClick={() => stopMutation.mutate(container.id)}
                        className="p-1.5 text-gray-400 hover:text-red-400 hover:bg-gray-700 rounded"
                        title="Stop"
                      >
                        <Square size={16} />
                      </button>
                    ) : (
                      <button
                        onClick={() => startMutation.mutate(container.id)}
                        className="p-1.5 text-gray-400 hover:text-green-400 hover:bg-gray-700 rounded"
                        title="Start"
                      >
                        <Play size={16} />
                      </button>
                    )}
                    <button
                      onClick={() => restartMutation.mutate(container.id)}
                      className="p-1.5 text-gray-400 hover:text-blue-400 hover:bg-gray-700 rounded"
                      title="Restart"
                    >
                      <RefreshCw size={16} />
                    </button>
                    <button
                      onClick={() => handleDelete(container)}
                      className="p-1.5 text-gray-400 hover:text-red-400 hover:bg-gray-700 rounded"
                      title="Delete"
                    >
                      <Trash2 size={16} />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {containers.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-gray-500">
                  No containers found. Create one to get started.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {showCreateModal && (
        <CreateContainerModal onClose={() => setShowCreateModal(false)} />
      )}
    </div>
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
