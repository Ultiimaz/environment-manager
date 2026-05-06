import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Plus, AlertCircle, ExternalLink } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { listProjects } from '@/services/api'
import type { Project } from '@/types'
import { AddProjectModal } from '@/components/projects/add-project-modal'
import { cn } from '@/lib/utils'

// Stitch design language applied to Projects list (no dedicated screen).
// Dense list rows: name + status pill + legacy badge, repo URL, default branch.

const STATUS_TOKENS = {
  active: { dot: 'bg-primary', text: 'text-primary', bg: 'bg-primary/10', border: 'border-primary/30' },
  archived: { dot: 'bg-muted-foreground', text: 'text-muted-foreground', bg: 'bg-secondary', border: 'border-border' },
  stale: { dot: 'bg-warning', text: 'text-warning', bg: 'bg-warning/10', border: 'border-warning/30' },
  default: { dot: 'bg-muted-foreground', text: 'text-muted-foreground', bg: 'bg-secondary', border: 'border-border' },
} as const

function StatusPill({ status }: { status: string }) {
  const tok = (STATUS_TOKENS as Record<string, (typeof STATUS_TOKENS)['default']>)[status] ?? STATUS_TOKENS.default
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10px] uppercase tracking-wider font-medium',
        tok.border,
        tok.bg,
        tok.text
      )}
    >
      <span className={cn('h-1.5 w-1.5 rounded-full', tok.dot)} />
      {status}
    </span>
  )
}

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

  useEffect(() => {
    load()
  }, [])

  return (
    <div className="px-6 py-6 space-y-6 max-w-[1400px] mx-auto">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-[28px] font-semibold tracking-tight leading-none">Projects</h1>
          <p className="mt-1.5 text-sm text-muted-foreground">
            <code className="font-mono text-xs text-foreground">.dev/</code>-based deploys. Push to
            deploy. One env per branch.
          </p>
        </div>
        <Button onClick={() => setModalOpen(true)} size="sm" className="h-8 shrink-0">
          <Plus className="h-3.5 w-3.5 mr-1.5" /> Add project
        </Button>
      </header>

      <div className="rounded-lg border border-border bg-card overflow-hidden">
        {loading && (
          <div className="px-4 py-6 text-[12px] text-muted-foreground">loading…</div>
        )}

        {error && (
          <div className="px-4 py-4 flex items-center gap-2 text-[12px] text-destructive border-l-2 border-destructive">
            <AlertCircle className="h-3.5 w-3.5 shrink-0" /> {error}
          </div>
        )}

        {!loading && !error && projects.length === 0 && (
          <div className="px-4 py-6 text-[12px] text-muted-foreground">
            No projects yet. Add one to get started.
          </div>
        )}

        {!loading && !error && projects.length > 0 && (
          <ul className="divide-y divide-border">
            {projects.map((p) => (
              <li key={p.id}>
                <Link
                  to={`/projects/${p.id}`}
                  className="grid grid-cols-[1fr_auto] items-center gap-4 px-4 py-3 hover:bg-secondary/40 transition-colors"
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2 mb-1 flex-wrap">
                      <span className="text-[14px] font-medium truncate">{p.name}</span>
                      {p.migrated_from_compose && (
                        <span className="inline-flex items-center rounded-full border border-border bg-secondary px-2 py-0.5 text-[10px] uppercase tracking-wider text-muted-foreground">
                          legacy
                        </span>
                      )}
                      <StatusPill status={p.status} />
                    </div>
                    <div className="text-[11px] text-muted-foreground/70 font-mono truncate inline-flex items-center gap-1">
                      {p.repo_url || '(no repo url — legacy)'}
                      {p.repo_url && <ExternalLink className="h-3 w-3 shrink-0" />}
                    </div>
                  </div>
                  <div className="flex items-center gap-3 text-[11px] text-muted-foreground/80 font-mono shrink-0">
                    <span>default: {p.default_branch || '—'}</span>
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>

      <AddProjectModal open={modalOpen} onClose={() => setModalOpen(false)} onCreated={load} />
    </div>
  )
}
