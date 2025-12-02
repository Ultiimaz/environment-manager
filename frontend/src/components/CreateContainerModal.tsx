import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { X, Plus, Trash2 } from 'lucide-react'
import { createContainer } from '../services/api'
import type { ContainerConfig, PortMapping, VolumeMount } from '../types'

interface Props {
  onClose: () => void
}

export default function CreateContainerModal({ onClose }: Props) {
  const [name, setName] = useState('')
  const [image, setImage] = useState('')
  const [ports, setPorts] = useState<PortMapping[]>([])
  const [volumes, setVolumes] = useState<VolumeMount[]>([])
  const [envVars, setEnvVars] = useState<{ key: string; value: string }[]>([])
  const [restart, setRestart] = useState('unless-stopped')

  const queryClient = useQueryClient()

  const createMutation = useMutation({
    mutationFn: () => {
      const config: ContainerConfig = {
        image,
        ports,
        volumes,
        env: envVars.reduce((acc, { key, value }) => {
          if (key) acc[key] = value
          return acc
        }, {} as Record<string, string>),
        restart,
      }
      return createContainer(name, config)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['containers'] })
      onClose()
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (name && image) {
      createMutation.mutate()
    }
  }

  const addPort = () => {
    setPorts([...ports, { host: 8080, container: 80, protocol: 'tcp' }])
  }

  const removePort = (index: number) => {
    setPorts(ports.filter((_, i) => i !== index))
  }

  const updatePort = (index: number, field: keyof PortMapping, value: number | string) => {
    setPorts(ports.map((p, i) => i === index ? { ...p, [field]: value } : p))
  }

  const addVolume = () => {
    setVolumes([...volumes, { name: '', container_path: '' }])
  }

  const removeVolume = (index: number) => {
    setVolumes(volumes.filter((_, i) => i !== index))
  }

  const updateVolume = (index: number, field: keyof VolumeMount, value: string | boolean) => {
    setVolumes(volumes.map((v, i) => i === index ? { ...v, [field]: value } : v))
  }

  const addEnvVar = () => {
    setEnvVars([...envVars, { key: '', value: '' }])
  }

  const removeEnvVar = (index: number) => {
    setEnvVars(envVars.filter((_, i) => i !== index))
  }

  const updateEnvVar = (index: number, field: 'key' | 'value', value: string) => {
    setEnvVars(envVars.map((e, i) => i === index ? { ...e, [field]: value } : e))
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 overflow-auto py-8">
      <div className="bg-gray-800 rounded-lg w-full max-w-2xl mx-4">
        <div className="flex items-center justify-between p-4 border-b border-gray-700">
          <h2 className="text-xl font-bold text-white">Create Container</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-white">
            <X size={24} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="p-4 space-y-6 max-h-[70vh] overflow-auto">
          {/* Basic Info */}
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm text-gray-400 mb-1">Container Name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:border-blue-500"
                placeholder="my-container"
                required
              />
            </div>
            <div>
              <label className="block text-sm text-gray-400 mb-1">Image</label>
              <input
                type="text"
                value={image}
                onChange={(e) => setImage(e.target.value)}
                className="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:border-blue-500"
                placeholder="nginx:latest"
                required
              />
            </div>
          </div>

          {/* Restart Policy */}
          <div>
            <label className="block text-sm text-gray-400 mb-1">Restart Policy</label>
            <select
              value={restart}
              onChange={(e) => setRestart(e.target.value)}
              className="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:border-blue-500"
            >
              <option value="no">No</option>
              <option value="always">Always</option>
              <option value="on-failure">On Failure</option>
              <option value="unless-stopped">Unless Stopped</option>
            </select>
          </div>

          {/* Ports */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <label className="text-sm text-gray-400">Port Mappings</label>
              <button type="button" onClick={addPort} className="text-sm text-blue-400 hover:text-blue-300 flex items-center gap-1">
                <Plus size={14} /> Add Port
              </button>
            </div>
            {ports.map((port, index) => (
              <div key={index} className="flex items-center gap-2 mb-2">
                <input
                  type="number"
                  value={port.host}
                  onChange={(e) => updatePort(index, 'host', parseInt(e.target.value))}
                  className="w-24 px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white"
                  placeholder="Host"
                />
                <span className="text-gray-400">:</span>
                <input
                  type="number"
                  value={port.container}
                  onChange={(e) => updatePort(index, 'container', parseInt(e.target.value))}
                  className="w-24 px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white"
                  placeholder="Container"
                />
                <select
                  value={port.protocol}
                  onChange={(e) => updatePort(index, 'protocol', e.target.value)}
                  className="px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white"
                >
                  <option value="tcp">TCP</option>
                  <option value="udp">UDP</option>
                </select>
                <button type="button" onClick={() => removePort(index)} className="text-red-400 hover:text-red-300">
                  <Trash2 size={18} />
                </button>
              </div>
            ))}
          </div>

          {/* Volumes */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <label className="text-sm text-gray-400">Volume Mounts</label>
              <button type="button" onClick={addVolume} className="text-sm text-blue-400 hover:text-blue-300 flex items-center gap-1">
                <Plus size={14} /> Add Volume
              </button>
            </div>
            {volumes.map((volume, index) => (
              <div key={index} className="flex items-center gap-2 mb-2">
                <input
                  type="text"
                  value={volume.name || volume.host_path || ''}
                  onChange={(e) => updateVolume(index, 'name', e.target.value)}
                  className="flex-1 px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white"
                  placeholder="Volume name or host path"
                />
                <span className="text-gray-400">:</span>
                <input
                  type="text"
                  value={volume.container_path}
                  onChange={(e) => updateVolume(index, 'container_path', e.target.value)}
                  className="flex-1 px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white"
                  placeholder="Container path"
                />
                <button type="button" onClick={() => removeVolume(index)} className="text-red-400 hover:text-red-300">
                  <Trash2 size={18} />
                </button>
              </div>
            ))}
          </div>

          {/* Environment Variables */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <label className="text-sm text-gray-400">Environment Variables</label>
              <button type="button" onClick={addEnvVar} className="text-sm text-blue-400 hover:text-blue-300 flex items-center gap-1">
                <Plus size={14} /> Add Variable
              </button>
            </div>
            {envVars.map((env, index) => (
              <div key={index} className="flex items-center gap-2 mb-2">
                <input
                  type="text"
                  value={env.key}
                  onChange={(e) => updateEnvVar(index, 'key', e.target.value)}
                  className="flex-1 px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white"
                  placeholder="KEY"
                />
                <span className="text-gray-400">=</span>
                <input
                  type="text"
                  value={env.value}
                  onChange={(e) => updateEnvVar(index, 'value', e.target.value)}
                  className="flex-1 px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white"
                  placeholder="value"
                />
                <button type="button" onClick={() => removeEnvVar(index)} className="text-red-400 hover:text-red-300">
                  <Trash2 size={18} />
                </button>
              </div>
            ))}
          </div>
        </form>

        <div className="flex justify-end gap-3 p-4 border-t border-gray-700">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 text-gray-300 hover:text-white"
          >
            Cancel
          </button>
          <button
            onClick={handleSubmit}
            disabled={createMutation.isPending || !name || !image}
            className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
          >
            {createMutation.isPending ? 'Creating...' : 'Create Container'}
          </button>
        </div>
      </div>
    </div>
  )
}
