import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Layers, Play, Square, Trash2, Plus } from 'lucide-react'
import {
  getComposeProjects,
  composeUp,
  composeDown,
  deleteComposeProject,
  createComposeProject
} from '../services/api'
import type { ComposeProject } from '../types'

export default function Compose() {
  const [showCreateModal, setShowCreateModal] = useState(false)
  const queryClient = useQueryClient()

  const { data: projects = [], isLoading } = useQuery({
    queryKey: ['composeProjects'],
    queryFn: getComposeProjects,
    refetchInterval: 10000,
  })

  const upMutation = useMutation({
    mutationFn: composeUp,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['composeProjects'] }),
  })

  const downMutation = useMutation({
    mutationFn: composeDown,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['composeProjects'] }),
  })

  const deleteMutation = useMutation({
    mutationFn: deleteComposeProject,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['composeProjects'] }),
  })

  const handleDelete = (project: ComposeProject) => {
    if (confirm(`Delete compose project "${project.project_name}"?`)) {
      deleteMutation.mutate(project.project_name)
    }
  }

  if (isLoading) {
    return <div className="p-6 text-gray-400">Loading...</div>
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-white">Compose Projects</h1>
        <button
          onClick={() => setShowCreateModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors"
        >
          <Plus size={20} />
          Import Project
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {projects.map(project => (
          <div key={project.project_name} className="bg-gray-800 rounded-lg p-4">
            <div className="flex items-start justify-between mb-3">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-purple-500/10 rounded-lg text-purple-400">
                  <Layers size={24} />
                </div>
                <div>
                  <h3 className="text-white font-medium">{project.project_name}</h3>
                  <p className="text-sm text-gray-400">
                    {project.services?.length || 0} services
                  </p>
                </div>
              </div>
              <span className={`px-2 py-1 rounded text-xs font-medium ${
                project.desired_state === 'running'
                  ? 'bg-green-500/20 text-green-400'
                  : 'bg-gray-500/20 text-gray-400'
              }`}>
                {project.desired_state}
              </span>
            </div>

            {project.services && project.services.length > 0 && (
              <div className="space-y-1 mb-4">
                {project.services.map(service => (
                  <div key={service.name} className="flex items-center justify-between text-sm">
                    <span className="text-gray-300">{service.name}</span>
                    <span className={`${
                      service.state === 'running' ? 'text-green-400' : 'text-gray-500'
                    }`}>
                      {service.state}
                    </span>
                  </div>
                ))}
              </div>
            )}

            <div className="flex items-center gap-2 pt-3 border-t border-gray-700">
              {project.desired_state === 'running' ? (
                <button
                  onClick={() => downMutation.mutate(project.project_name)}
                  className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-300 hover:text-red-400 hover:bg-gray-700 rounded"
                >
                  <Square size={14} />
                  Down
                </button>
              ) : (
                <button
                  onClick={() => upMutation.mutate(project.project_name)}
                  className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-300 hover:text-green-400 hover:bg-gray-700 rounded"
                >
                  <Play size={14} />
                  Up
                </button>
              )}
              <button
                onClick={() => handleDelete(project)}
                className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-300 hover:text-red-400 hover:bg-gray-700 rounded"
              >
                <Trash2 size={14} />
                Delete
              </button>
            </div>
          </div>
        ))}

        {projects.length === 0 && (
          <div className="col-span-full text-center py-12 text-gray-500">
            No compose projects found. Import one to get started.
          </div>
        )}
      </div>

      {showCreateModal && (
        <CreateComposeModal onClose={() => setShowCreateModal(false)} />
      )}
    </div>
  )
}

function CreateComposeModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const [yaml, setYaml] = useState(`version: '3.8'

services:
  web:
    image: nginx:latest
    ports:
      - "80:80"
`)
  const queryClient = useQueryClient()

  const createMutation = useMutation({
    mutationFn: () => createComposeProject(name, yaml),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['composeProjects'] })
      onClose()
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (name && yaml) {
      createMutation.mutate()
    }
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-gray-800 rounded-lg p-6 w-full max-w-2xl max-h-[80vh] overflow-auto">
        <h2 className="text-xl font-bold text-white mb-4">Import Compose Project</h2>

        <form onSubmit={handleSubmit}>
          <div className="space-y-4">
            <div>
              <label className="block text-sm text-gray-400 mb-1">Project Name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:border-blue-500"
                placeholder="my-project"
                required
              />
            </div>

            <div>
              <label className="block text-sm text-gray-400 mb-1">docker-compose.yaml</label>
              <textarea
                value={yaml}
                onChange={(e) => setYaml(e.target.value)}
                className="w-full h-64 px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white font-mono text-sm focus:outline-none focus:border-blue-500"
                required
              />
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
