import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Plus, GitBranch, AlertCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { getProjects } from '@/services/api'
import type { Project } from '@/types'
import { AddProjectModal } from '@/components/projects/add-project-modal'

export default function Projects() {
  const [projects, setProjects] = useState<Project[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [modalOpen, setModalOpen] = useState(false)

  async function load() {
    try {
      setError(null)
      const list = await getProjects()
      setProjects(list)
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'Failed to load projects'
      setError(msg)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold">Projects</h1>
          <p className="text-muted-foreground">
            .dev/-based deploys. Push to deploy. One env per branch.
          </p>
        </div>
        <Button onClick={() => setModalOpen(true)}>
          <Plus className="h-4 w-4 mr-2" /> Add project
        </Button>
      </div>

      {loading && <p>Loading...</p>}
      {error && (
        <Card className="mb-4 border-destructive">
          <CardContent className="pt-6 flex items-center gap-2 text-destructive">
            <AlertCircle className="h-4 w-4" /> {error}
          </CardContent>
        </Card>
      )}
      {!loading && !error && projects.length === 0 && (
        <Card>
          <CardContent className="pt-6 text-center text-muted-foreground">
            No projects yet. Add one to get started.
          </CardContent>
        </Card>
      )}
      <div className="grid gap-4">
        {projects.map(p => (
          <Link key={p.id} to={`/projects/${p.id}`}>
            <Card className="hover:bg-accent transition-colors">
              <CardHeader>
                <CardTitle className="flex items-center justify-between">
                  <span className="flex items-center gap-2">
                    <GitBranch className="h-5 w-5" />
                    {p.name}
                  </span>
                  <div className="flex gap-2">
                    {p.migrated_from_compose && (
                      <Badge variant="secondary">legacy</Badge>
                    )}
                    <Badge variant={p.status === 'active' ? 'default' : 'outline'}>
                      {p.status}
                    </Badge>
                  </div>
                </CardTitle>
              </CardHeader>
              <CardContent className="text-sm text-muted-foreground">
                <div>{p.repo_url || '(no repo url — legacy)'}</div>
                <div>default branch: {p.default_branch || '—'}</div>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>

      <AddProjectModal open={modalOpen} onClose={() => setModalOpen(false)} onCreated={load} />
    </div>
  )
}
