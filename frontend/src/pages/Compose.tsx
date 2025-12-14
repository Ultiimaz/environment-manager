import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Layers, Play, Square, Trash2, Plus, Search, FileCode } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  getComposeProjects,
  composeUp,
  composeDown,
  deleteComposeProject,
  createComposeProject,
} from '../services/api'
import type { ComposeProject } from '../types'

export default function Compose() {
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [projectToDelete, setProjectToDelete] = useState<ComposeProject | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
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
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['composeProjects'] })
      setDeleteDialogOpen(false)
      setProjectToDelete(null)
    },
  })

  const handleDeleteClick = (project: ComposeProject) => {
    setProjectToDelete(project)
    setDeleteDialogOpen(true)
  }

  const handleConfirmDelete = () => {
    if (projectToDelete) {
      deleteMutation.mutate(projectToDelete.project_name)
    }
  }

  const filteredProjects = projects.filter((project) =>
    project.project_name.toLowerCase().includes(searchQuery.toLowerCase())
  )

  const runningCount = projects.filter((p) => p.desired_state === 'running').length

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold">Compose Projects</h1>
          <p className="text-muted-foreground">
            {runningCount} running, {projects.length - runningCount} stopped
          </p>
        </div>
        <Button onClick={() => setShowCreateDialog(true)}>
          <Plus className="h-4 w-4 mr-2" />
          Import Project
        </Button>
      </div>

      <div className="relative max-w-sm">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          placeholder="Search projects..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="pl-9"
        />
      </div>

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {[...Array(6)].map((_, i) => (
            <Skeleton key={i} className="h-56" />
          ))}
        </div>
      ) : filteredProjects.length === 0 ? (
        <div className="text-center py-12 text-muted-foreground">
          {searchQuery ? 'No projects match your search' : 'No compose projects found. Import one to get started.'}
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {filteredProjects.map((project) => (
            <Card key={project.project_name}>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-3">
                    <div className="p-2 bg-warning/10 rounded-lg text-warning">
                      <Layers className="h-6 w-6" />
                    </div>
                    <div>
                      <CardTitle className="text-base">{project.project_name}</CardTitle>
                      <p className="text-sm text-muted-foreground">
                        {project.services?.length || 0} services
                      </p>
                    </div>
                  </div>
                  <Badge variant={project.desired_state === 'running' ? 'success' : 'secondary'}>
                    {project.desired_state}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent>
                {project.services && project.services.length > 0 && (
                  <div className="space-y-2 mb-4">
                    {project.services.map((service) => (
                      <div key={service.name} className="flex items-center justify-between text-sm">
                        <span>{service.name}</span>
                        <Badge
                          variant={service.state === 'running' ? 'success' : 'secondary'}
                          className="text-xs"
                        >
                          {service.state}
                        </Badge>
                      </div>
                    ))}
                  </div>
                )}

                <Separator className="my-4" />

                <div className="flex items-center gap-2">
                  {project.desired_state === 'running' ? (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => downMutation.mutate(project.project_name)}
                      disabled={downMutation.isPending}
                    >
                      <Square className="h-4 w-4 mr-2" />
                      Down
                    </Button>
                  ) : (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => upMutation.mutate(project.project_name)}
                      disabled={upMutation.isPending}
                    >
                      <Play className="h-4 w-4 mr-2" />
                      Up
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive hover:text-destructive"
                    onClick={() => handleDeleteClick(project)}
                  >
                    <Trash2 className="h-4 w-4 mr-2" />
                    Delete
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <CreateComposeDialog open={showCreateDialog} onOpenChange={setShowCreateDialog} />

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Compose Project</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete &quot;{projectToDelete?.project_name}&quot;? This will stop all services and remove the project configuration.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleConfirmDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function CreateComposeDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
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
      onOpenChange(false)
      setName('')
      setYaml(`version: '3.8'

services:
  web:
    image: nginx:latest
    ports:
      - "80:80"
`)
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (name && yaml) {
      createMutation.mutate()
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Import Compose Project</DialogTitle>
          <DialogDescription>Create a new Docker Compose project from a YAML configuration.</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <label className="text-sm font-medium">Project Name</label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="my-project"
                required
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium flex items-center gap-2">
                <FileCode className="h-4 w-4" />
                docker-compose.yaml
              </label>
              <textarea
                value={yaml}
                onChange={(e) => setYaml(e.target.value)}
                className="w-full h-64 px-3 py-2 rounded-md border border-input bg-transparent font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                required
              />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
