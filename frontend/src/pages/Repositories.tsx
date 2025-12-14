import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { GitBranch, Plus, RefreshCw, Trash2, FileCode, Clock, Lock, Unlock, Rocket } from 'lucide-react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  getRepositories,
  cloneRepository,
  pullRepository,
  deleteRepository,
  getRepositoryFileContent,
  createComposeProject,
  composeUp,
  checkSubdomainAvailability,
} from '../services/api'
import type { Repository, CloneRequest, ServiceSubdomain } from '../types'

export default function Repositories() {
  const [cloneDialogOpen, setCloneDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [setupDialogOpen, setSetupDialogOpen] = useState(false)
  const [repoToDelete, setRepoToDelete] = useState<Repository | null>(null)
  const [repoToSetup, setRepoToSetup] = useState<Repository | null>(null)
  const queryClient = useQueryClient()

  const { data: repositoriesData, isLoading } = useQuery({
    queryKey: ['repositories'],
    queryFn: getRepositories,
  })
  const repositories = repositoriesData ?? []

  const pullMutation = useMutation({
    mutationFn: pullRepository,
    onSuccess: (repo) => {
      queryClient.invalidateQueries({ queryKey: ['repositories'] })
      if (repo.compose_files && repo.compose_files.length > 0) {
        setRepoToSetup(repo)
        setSetupDialogOpen(true)
      }
    },
  })

  const deleteMutation = useMutation({
    mutationFn: deleteRepository,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['repositories'] })
      setDeleteDialogOpen(false)
      setRepoToDelete(null)
    },
  })

  const handleDeleteClick = (repo: Repository) => {
    setRepoToDelete(repo)
    setDeleteDialogOpen(true)
  }

  const handleConfirmDelete = () => {
    if (repoToDelete) {
      deleteMutation.mutate(repoToDelete.id)
    }
  }

  const handleSetupClick = (repo: Repository) => {
    setRepoToSetup(repo)
    setSetupDialogOpen(true)
  }

  const handleCloneSuccess = (repo: Repository) => {
    if (repo.compose_files && repo.compose_files.length > 0) {
      setRepoToSetup(repo)
      setSetupDialogOpen(true)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold">Repositories</h1>
          <p className="text-muted-foreground">
            {repositories.length} {repositories.length === 1 ? 'repository' : 'repositories'} cloned
          </p>
        </div>
        <Button onClick={() => setCloneDialogOpen(true)}>
          <Plus className="h-4 w-4 mr-2" />
          Clone Repository
        </Button>
      </div>

      {isLoading ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[...Array(3)].map((_, i) => (
            <Card key={i}>
              <CardHeader>
                <Skeleton className="h-6 w-3/4" />
                <Skeleton className="h-4 w-1/2" />
              </CardHeader>
              <CardContent>
                <Skeleton className="h-20 w-full" />
              </CardContent>
            </Card>
          ))}
        </div>
      ) : repositories.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <GitBranch className="h-12 w-12 text-muted-foreground mb-4" />
            <h3 className="text-lg font-medium mb-2">No repositories</h3>
            <p className="text-muted-foreground text-center mb-4">
              Clone a Git repository to get started with deployments.
            </p>
            <Button onClick={() => setCloneDialogOpen(true)}>
              <Plus className="h-4 w-4 mr-2" />
              Clone Repository
            </Button>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {repositories.map((repo) => (
            <RepositoryCard
              key={repo.id}
              repo={repo}
              onPull={() => pullMutation.mutate(repo.id)}
              onDelete={() => handleDeleteClick(repo)}
              onSetup={() => handleSetupClick(repo)}
              isPulling={pullMutation.isPending && pullMutation.variables === repo.id}
            />
          ))}
        </div>
      )}

      <CloneDialog
        open={cloneDialogOpen}
        onOpenChange={setCloneDialogOpen}
        onSuccess={handleCloneSuccess}
      />

      <SetupComposeDialog
        open={setupDialogOpen}
        onOpenChange={setSetupDialogOpen}
        repo={repoToSetup}
      />

      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Repository</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete "{repoToDelete?.name}"? This will remove the local
              clone. This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleConfirmDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function RepositoryCard({
  repo,
  onPull,
  onDelete,
  onSetup,
  isPulling,
}: {
  repo: Repository
  onPull: () => void
  onDelete: () => void
  onSetup: () => void
  isPulling: boolean
}) {
  const formatDate = (dateString: string) => {
    const date = new Date(dateString)
    return date.toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    })
  }

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2">
              <GitBranch className="h-4 w-4" />
              {repo.name}
            </CardTitle>
            <CardDescription className="font-mono text-xs break-all">
              {repo.url}
            </CardDescription>
          </div>
          <span title={repo.has_token ? "Private repository" : "Public repository"}>
            {repo.has_token ? (
              <Lock className="h-4 w-4 text-muted-foreground" />
            ) : (
              <Unlock className="h-4 w-4 text-muted-foreground" />
            )}
          </span>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Badge variant="outline">{repo.branch}</Badge>
          <span className="flex items-center gap-1">
            <Clock className="h-3 w-3" />
            Pulled {formatDate(repo.last_pulled)}
          </span>
        </div>

        {repo.compose_files && repo.compose_files.length > 0 && (
          <div className="space-y-2">
            <div className="text-sm font-medium flex items-center gap-1">
              <FileCode className="h-4 w-4" />
              Compose Files
            </div>
            <div className="flex flex-wrap gap-1">
              {repo.compose_files.map((file) => (
                <Badge key={file} variant="secondary" className="font-mono text-xs">
                  {file}
                </Badge>
              ))}
            </div>
          </div>
        )}

        <div className="flex gap-2 pt-2">
          <Button
            variant="outline"
            size="sm"
            onClick={onPull}
            disabled={isPulling}
            className="flex-1"
          >
            <RefreshCw className={`h-4 w-4 mr-2 ${isPulling ? 'animate-spin' : ''}`} />
            {isPulling ? 'Pulling...' : 'Pull'}
          </Button>
          {repo.compose_files && repo.compose_files.length > 0 && (
            <Button
              variant="default"
              size="sm"
              onClick={onSetup}
            >
              <Rocket className="h-4 w-4" />
            </Button>
          )}
          <Button
            variant="outline"
            size="sm"
            onClick={onDelete}
            className="text-destructive hover:text-destructive"
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

function CloneDialog({
  open,
  onOpenChange,
  onSuccess,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: (repo: Repository) => void
}) {
  const [url, setUrl] = useState('')
  const [branch, setBranch] = useState('')
  const [token, setToken] = useState('')
  const queryClient = useQueryClient()

  const cloneMutation = useMutation({
    mutationFn: (req: CloneRequest) => cloneRepository(req),
    onSuccess: (repo) => {
      queryClient.invalidateQueries({ queryKey: ['repositories'] })
      onOpenChange(false)
      resetForm()
      onSuccess(repo)
    },
  })

  const resetForm = () => {
    setUrl('')
    setBranch('')
    setToken('')
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (url) {
      cloneMutation.mutate({
        url,
        branch: branch || undefined,
        token: token || undefined,
      })
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Clone Repository</DialogTitle>
          <DialogDescription>
            Clone a Git repository. For private repositories, provide a Personal Access Token.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="url">Repository URL</Label>
              <Input
                id="url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://github.com/user/repo.git"
                required
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="branch">Branch (optional)</Label>
              <Input
                id="branch"
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                placeholder="main"
              />
              <p className="text-xs text-muted-foreground">
                Leave empty to use the default branch
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="token">Personal Access Token (optional)</Label>
              <Input
                id="token"
                type="password"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="ghp_xxxxxxxxxxxx"
              />
              <p className="text-xs text-muted-foreground">
                Required for private repositories. Token is encrypted and stored securely.
              </p>
            </div>
          </div>

          {cloneMutation.isError && (
            <p className="text-sm text-destructive mb-4">
              {(cloneMutation.error as Error)?.message || 'Failed to clone repository'}
            </p>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={cloneMutation.isPending || !url}>
              {cloneMutation.isPending ? 'Cloning...' : 'Clone'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function SetupComposeDialog({
  open,
  onOpenChange,
  repo,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  repo: Repository | null
}) {
  const [selectedFile, setSelectedFile] = useState<string>('')
  const [composeContent, setComposeContent] = useState<string>('')
  const [envVars, setEnvVars] = useState<Record<string, string>>({})
  const [projectName, setProjectName] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [subdomainMappings, setSubdomainMappings] = useState<Array<{ service: string; port: number; subdomain: string }>>([])
  const [subdomainErrors, setSubdomainErrors] = useState<Record<string, string>>({})
  const queryClient = useQueryClient()

  // Reset when dialog opens with new repo
  useEffect(() => {
    if (open && repo) {
      setProjectName(repo.name)
      if (repo.compose_files && repo.compose_files.length > 0) {
        setSelectedFile(repo.compose_files[0])
      }
    } else {
      setSelectedFile('')
      setComposeContent('')
      setEnvVars({})
      setProjectName('')
      setSubdomainMappings([])
      setSubdomainErrors({})
    }
  }, [open, repo])

  // Load compose file content when selected or dialog opens
  useEffect(() => {
    if (open && repo && selectedFile) {
      setIsLoading(true)
      getRepositoryFileContent(repo.id, selectedFile)
        .then((content) => {
          setComposeContent(content)
          // Parse environment variables from compose file
          const vars = parseEnvVars(content)
          setEnvVars(vars)
          // Parse services from compose file for subdomain configuration
          const services = parseServices(content)
          setSubdomainMappings(services.map(s => ({
            service: s.service,
            port: s.port,
            subdomain: '',
          })))
          setSubdomainErrors({})
        })
        .catch(console.error)
        .finally(() => setIsLoading(false))
    }
  }, [open, repo, selectedFile])

  const deployMutation = useMutation({
    mutationFn: async () => {
      if (!repo || !composeContent) return

      // Replace environment variables in compose content
      let finalContent = composeContent
      for (const [key, value] of Object.entries(envVars)) {
        finalContent = finalContent.replace(new RegExp(`\\$\\{${key}\\}`, 'g'), value)
        finalContent = finalContent.replace(new RegExp(`\\$${key}`, 'g'), value)
      }

      // Build subdomain configuration for services that have a subdomain set
      const subdomains: Record<string, ServiceSubdomain> = {}
      for (const mapping of subdomainMappings) {
        if (mapping.subdomain.trim()) {
          subdomains[mapping.service] = {
            subdomain: mapping.subdomain.trim(),
            port: mapping.port,
          }
        }
      }

      // Create compose project with subdomain configuration
      await createComposeProject(projectName, finalContent, subdomains)
      // Start it
      await composeUp(projectName)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['composeProjects'] })
      onOpenChange(false)
    },
  })

  // Reset mutation state when dialog opens
  useEffect(() => {
    if (open) {
      deployMutation.reset()
    }
  }, [open])

  const handleDeploy = () => {
    deployMutation.mutate()
  }

  if (!repo) return null

  const hasEnvVars = Object.keys(envVars).length > 0

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Setup Compose Project</DialogTitle>
          <DialogDescription>
            We found docker-compose files in {repo.name}. Configure and deploy your project.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="projectName">Project Name</Label>
            <Input
              id="projectName"
              value={projectName}
              onChange={(e) => setProjectName(e.target.value)}
              placeholder="my-project"
            />
          </div>

          {repo.compose_files && repo.compose_files.length > 1 && (
            <div className="space-y-2">
              <Label>Compose File</Label>
              <Select value={selectedFile} onValueChange={setSelectedFile}>
                <SelectTrigger>
                  <SelectValue placeholder="Select compose file" />
                </SelectTrigger>
                <SelectContent>
                  {repo.compose_files.map((file) => (
                    <SelectItem key={file} value={file}>
                      {file}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {isLoading ? (
            <div className="space-y-2">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : (
            <>
              {hasEnvVars && (
                <div className="space-y-3">
                  <Label>Environment Variables</Label>
                  <p className="text-xs text-muted-foreground">
                    Fill in the required environment variables for your compose file.
                  </p>
                  {Object.entries(envVars).map(([key, value]) => (
                    <div key={key} className="space-y-1">
                      <Label htmlFor={key} className="text-xs font-mono">
                        {key}
                      </Label>
                      <Input
                        id={key}
                        value={value}
                        onChange={(e) => setEnvVars((prev) => ({ ...prev, [key]: e.target.value }))}
                        placeholder={`Enter ${key}`}
                      />
                    </div>
                  ))}
                </div>
              )}

              {subdomainMappings.length > 0 && (
                <div className="space-y-3">
                  <Label>Subdomains</Label>
                  <p className="text-xs text-muted-foreground">
                    Configure subdomains for your services. Leave empty for services you don't need to access via subdomain.
                  </p>
                  <div className="space-y-3">
                    {subdomainMappings.map((mapping, index) => (
                      <div key={mapping.service} className="space-y-1">
                        <div className="flex items-center gap-2 text-sm">
                          <span className="text-muted-foreground w-24 truncate" title={mapping.service}>
                            {mapping.service}
                          </span>
                          <div className="flex items-center flex-1">
                            <Input
                              type="text"
                              value={mapping.subdomain}
                              onChange={(e) => {
                                const value = e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '')
                                const newMappings = [...subdomainMappings]
                                newMappings[index] = { ...mapping, subdomain: value }
                                setSubdomainMappings(newMappings)
                                // Clear error when user types
                                if (subdomainErrors[mapping.service]) {
                                  setSubdomainErrors(prev => {
                                    const next = { ...prev }
                                    delete next[mapping.service]
                                    return next
                                  })
                                }
                              }}
                              onBlur={async () => {
                                if (mapping.subdomain.trim()) {
                                  try {
                                    const result = await checkSubdomainAvailability(mapping.subdomain)
                                    if (!result.available) {
                                      setSubdomainErrors(prev => ({
                                        ...prev,
                                        [mapping.service]: 'Subdomain is already in use'
                                      }))
                                    }
                                  } catch {
                                    // Ignore check errors
                                  }
                                }
                              }}
                              placeholder="my-service"
                              className="font-mono rounded-r-none"
                            />
                            <span className="inline-flex items-center px-3 h-10 bg-muted border border-l-0 border-input rounded-r-md text-sm text-muted-foreground">
                              .home
                            </span>
                          </div>
                          <span className="text-xs text-muted-foreground">
                            :{mapping.port}
                          </span>
                        </div>
                        {subdomainErrors[mapping.service] && (
                          <p className="text-xs text-destructive ml-26">
                            {subdomainErrors[mapping.service]}
                          </p>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {!hasEnvVars && subdomainMappings.length === 0 && composeContent && (
                <p className="text-sm text-muted-foreground">
                  No environment variables or services detected. Ready to deploy.
                </p>
              )}
            </>
          )}
        </div>

        {deployMutation.isError && (
          <p className="text-sm text-destructive mb-4">
            {(deployMutation.error as Error)?.message || 'Failed to deploy'}
          </p>
        )}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            onClick={handleDeploy}
            disabled={deployMutation.isPending || !projectName || !composeContent}
          >
            <Rocket className="h-4 w-4 mr-2" />
            {deployMutation.isPending ? 'Deploying...' : 'Deploy'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Parse environment variables from compose file content
function parseEnvVars(content: string): Record<string, string> {
  const vars: Record<string, string> = {}

  // Match ${VAR_NAME} and $VAR_NAME patterns
  const pattern = /\$\{([A-Z_][A-Z0-9_]*)\}|\$([A-Z_][A-Z0-9_]*)/g
  let match

  while ((match = pattern.exec(content)) !== null) {
    const varName = match[1] || match[2]
    // Skip common docker compose variables
    if (!['PWD', 'HOME', 'USER', 'PATH'].includes(varName)) {
      vars[varName] = ''
    }
  }

  return vars
}

// Parse services and their exposed ports from compose file content
function parseServices(content: string): Array<{ service: string; port: number }> {
  const services: Array<{ service: string; port: number }> = []
  const seenServices = new Set<string>()

  // Simple YAML parsing for services and their ports
  const lines = content.split('\n')
  let currentService = ''
  let inPorts = false
  let inExpose = false

  for (const line of lines) {
    // Detect service name (indented with 2 spaces under services:)
    const serviceMatch = line.match(/^  ([a-zA-Z0-9_-]+):/)
    if (serviceMatch) {
      currentService = serviceMatch[1]
      inPorts = false
      inExpose = false
      continue
    }

    // Detect ports section
    if (line.match(/^\s+ports:\s*$/)) {
      inPorts = true
      inExpose = false
      continue
    }

    // Detect expose section
    if (line.match(/^\s+expose:\s*$/)) {
      inExpose = true
      inPorts = false
      continue
    }

    // Detect end of ports/expose section (new section at same or higher indent level)
    if ((inPorts || inExpose) && line.match(/^\s{4}[a-z_]+:/) && !line.includes('- ')) {
      inPorts = false
      inExpose = false
      continue
    }

    // Parse port mapping (host:container or just container)
    if (inPorts && currentService && !seenServices.has(currentService)) {
      // Match "8080:80", "8080", or quoted versions
      const portMatch = line.match(/^\s+-\s*["']?(?:\d+:)?(\d+)(?:\/\w+)?["']?/)
      if (portMatch) {
        services.push({
          service: currentService,
          port: parseInt(portMatch[1], 10),
        })
        seenServices.add(currentService)
      }
    }

    // Parse expose port (just container port)
    if (inExpose && currentService && !seenServices.has(currentService)) {
      const exposeMatch = line.match(/^\s+-\s*["']?(\d+)["']?/)
      if (exposeMatch) {
        services.push({
          service: currentService,
          port: parseInt(exposeMatch[1], 10),
        })
        seenServices.add(currentService)
      }
    }
  }

  return services
}
