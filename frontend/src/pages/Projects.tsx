import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Plus, AlertCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Section } from '@/components/ui/section'
import { Badge } from '@/components/ui/badge'
import { listProjects } from '@/services/api'
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
      const list = await listProjects()
      setProjects(list as Project[])
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'Failed to load projects'
      setError(msg)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  return (
    <div className="p-6 space-y-4 max-w-5xl">
      <header className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold">Projects</h1>
          <p className="text-sm text-muted-foreground">
            <code className="font-mono text-xs">.dev/</code>-based deploys. Push to deploy. One env per branch.
          </p>
        </div>
        <Button onClick={() => setModalOpen(true)} size="sm">
          <Plus className="h-4 w-4 mr-1.5" /> Add project
        </Button>
      </header>

      {loading && (
        <Section>
          <div className="text-xs text-muted-foreground">loading…</div>
        </Section>
      )}

      {error && (
        <Section className="border-red-900/60">
          <div className="flex items-center gap-2 text-xs text-red-400">
            <AlertCircle className="h-4 w-4 shrink-0" /> {error}
          </div>
        </Section>
      )}

      {!loading && !error && projects.length === 0 && (
        <Section>
          <div className="text-xs text-muted-foreground">No projects yet. Add one to get started.</div>
        </Section>
      )}

      {!loading && !error && projects.length > 0 && (
        <Section flush className="p-0 overflow-hidden">
          <ul className="divide-y divide-border">
            {projects.map((p) => (
              <li key={p.id}>
                <Link
                  to={`/projects/${p.id}`}
                  className="flex items-center justify-between gap-4 px-4 py-3 hover:bg-muted/40 transition-colors"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 mb-0.5">
                      <span className="text-sm font-medium truncate">{p.name}</span>
                      {p.migrated_from_compose && (
                        <Badge variant="default">legacy</Badge>
                      )}
                      <Badge variant={p.status === 'active' ? 'success' : 'default'}>
                        {p.status}
                      </Badge>
                    </div>
                    <div className="text-xs text-muted-foreground font-mono truncate">
                      {p.repo_url || '(no repo url — legacy)'}
                    </div>
                  </div>
                  <span className="text-xs text-muted-foreground font-mono shrink-0">
                    {p.default_branch || '—'}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </Section>
      )}

      <AddProjectModal open={modalOpen} onClose={() => setModalOpen(false)} onCreated={load} />
    </div>
  )
}
