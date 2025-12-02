import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { HardDrive, Trash2, Plus, Download } from 'lucide-react'
import { getVolumes, deleteVolume, backupVolume, createVolume } from '../services/api'
import type { Volume } from '../types'

export default function Volumes() {
  const [showCreateModal, setShowCreateModal] = useState(false)
  const queryClient = useQueryClient()

  const { data: volumes = [], isLoading } = useQuery({
    queryKey: ['volumes'],
    queryFn: getVolumes,
  })

  const deleteMutation = useMutation({
    mutationFn: deleteVolume,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['volumes'] }),
  })

  const backupMutation = useMutation({
    mutationFn: backupVolume,
    onSuccess: () => {
      alert('Backup started')
    },
  })

  const handleDelete = (volume: Volume) => {
    if (confirm(`Delete volume "${volume.name}"? This cannot be undone.`)) {
      deleteMutation.mutate(volume.name)
    }
  }

  const formatBytes = (bytes?: number) => {
    if (!bytes) return '-'
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${sizes[i]}`
  }

  if (isLoading) {
    return <div className="p-6 text-gray-400">Loading...</div>
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-white">Volumes</h1>
        <button
          onClick={() => setShowCreateModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors"
        >
          <Plus size={20} />
          Create Volume
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {volumes.map(volume => (
          <div key={volume.name} className="bg-gray-800 rounded-lg p-4">
            <div className="flex items-start justify-between mb-3">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-blue-500/10 rounded-lg text-blue-400">
                  <HardDrive size={24} />
                </div>
                <div>
                  <h3 className="text-white font-medium">{volume.name}</h3>
                  <p className="text-sm text-gray-400">{volume.driver}</p>
                </div>
              </div>
              {volume.is_managed && (
                <span className="text-xs text-green-400">managed</span>
              )}
            </div>

            <div className="space-y-2 text-sm mb-4">
              <div className="flex justify-between">
                <span className="text-gray-400">Size</span>
                <span className="text-gray-300">{formatBytes(volume.size_bytes)}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-gray-400">Mountpoint</span>
                <span className="text-gray-300 font-mono text-xs truncate max-w-[180px]" title={volume.mountpoint}>
                  {volume.mountpoint}
                </span>
              </div>
            </div>

            <div className="flex items-center gap-2 pt-3 border-t border-gray-700">
              <button
                onClick={() => backupMutation.mutate(volume.name)}
                className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-300 hover:text-white hover:bg-gray-700 rounded"
              >
                <Download size={14} />
                Backup
              </button>
              <button
                onClick={() => handleDelete(volume)}
                className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-300 hover:text-red-400 hover:bg-gray-700 rounded"
              >
                <Trash2 size={14} />
                Delete
              </button>
            </div>
          </div>
        ))}

        {volumes.length === 0 && (
          <div className="col-span-full text-center py-12 text-gray-500">
            No volumes found. Create one to get started.
          </div>
        )}
      </div>

      {showCreateModal && (
        <CreateVolumeModal onClose={() => setShowCreateModal(false)} />
      )}
    </div>
  )
}

function CreateVolumeModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const [driver, setDriver] = useState('local')
  const queryClient = useQueryClient()

  const createMutation = useMutation({
    mutationFn: () => createVolume(name, driver),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['volumes'] })
      onClose()
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (name) {
      createMutation.mutate()
    }
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-gray-800 rounded-lg p-6 w-full max-w-md">
        <h2 className="text-xl font-bold text-white mb-4">Create Volume</h2>

        <form onSubmit={handleSubmit}>
          <div className="space-y-4">
            <div>
              <label className="block text-sm text-gray-400 mb-1">Name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:border-blue-500"
                placeholder="my-volume"
                required
              />
            </div>

            <div>
              <label className="block text-sm text-gray-400 mb-1">Driver</label>
              <select
                value={driver}
                onChange={(e) => setDriver(e.target.value)}
                className="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:border-blue-500"
              >
                <option value="local">local</option>
              </select>
            </div>
          </div>

          <div className="flex justify-end gap-3 mt-6">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-gray-300 hover:text-white"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={createMutation.isPending}
              className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
            >
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
