import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, RefreshCw, Play, ExternalLink } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Section } from '@/components/ui/section'
import { Badge } from '@/components/ui/badge'
import { getProject, triggerBuild } from '@/services/api'
import type { ProjectDetail, Environment } from '@/services/api'
import { BuildLogViewer } from '@/components/projects/build-log-viewer'

function envStatusVariant(status: string): 'success' | 'failed' | 'pending' | 'default' {
  switch (status) {
    case 'running': return 'success'
    case 'failed': return 'failed'
    case 'building': return 'pending'
    default: return 'default'
  }
}

export default function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>()
  const [data, setData] = useState<ProjectDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [logEnvId, setLogEnvId] = useState<string | null>(null)
  const [triggering, setTriggering] = useState<string | null>(null)

  async function load() {
    if (!id) return
    try {
      setError(null)
      const d = await getProject(id)
      setData(d)
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'Failed to load project'
      setError(msg)
    }
  }

  useEffect(() => { load() }, [id])

  // Poll while any env is building.
  useEffect(() => {
    if (!data) return
    const anyBuilding = data.environments.some(e => e.status === 'building')
    if (!anyBuilding) return
    const t = setInterval(load, 3000)
    return () => clearInterval(t)
  }, [data])

  async function onBuild(envId: string) {
    setTriggering(envId)
    try {
      await triggerBuild(envId)
      setLogEnvId(envId)
      await load()
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'Trigger failed'
      alert(msg)
    } finally {
      setTriggering(null)
    }
  }

  if (error) return <div className="p-6 text-sm text-red-400">{error}</div>
  if (!data) return <div className="p-6 text-sm text-muted-foreground">Loading…</div>

  const { project, environments } = data
  const sortedEnvs = [...environments].sort((a, b) => {
    if (a.kind === 'prod' && b.kind !== 'prod') return -1
    if (b.kind === 'prod' && a.kind !== 'prod') return 1
    return a.branch_slug.localeCompare(b.branch_slug)
  })

  return (
    <div className="p-6 space-y-4 max-w-5xl">
      <Link to="/projects" className="inline-flex items-center text-xs text-muted-foreground hover:text-foreground transition-colors">
        <ArrowLeft className="h-3 w-3 mr-1" /> Projects
      </Link>

      <header className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold">{project.name}</h1>
          <p className="text-xs text-muted-foreground font-mono">{project.repo_url}</p>
          <p className="text-xs text-muted-foreground">default: {project.default_branch}</p>
        </div>
        <Button variant="outline" size="sm" onClick={load}>
          <RefreshCw className="h-4 w-4 mr-1.5" /> Refresh
        </Button>
      </header>

      <Section
        title={`Environments (${sortedEnvs.length})`}
        flush
      >
        <ul className="divide-y divide-border -mx-4 -mb-1">
          {sortedEnvs.map(e => (
            <EnvRow
              key={e.id}
              env={e}
              projectId={project.id}
              triggering={triggering === e.id}
              onBuild={() => onBuild(e.id)}
              onShowLog={() => setLogEnvId(e.id)}
            />
          ))}
        </ul>
      </Section>

      {logEnvId && (
        <Section
          title={
            <span className="font-mono text-xs">Build log · {logEnvId}</span>
          }
          action={
            <Button variant="ghost" size="sm" onClick={() => setLogEnvId(null)}>
              Close
            </Button>
          }
        >
          <BuildLogViewer envId={logEnvId} />
        </Section>
      )}
    </div>
  )
}

interface EnvRowProps {
  env: Environment
  projectId: string
  triggering: boolean
  onBuild: () => void
  onShowLog: () => void
}

function EnvRow({ env, projectId, triggering, onBuild, onShowLog }: EnvRowProps) {
  return (
    <div className="flex items-center justify-between gap-4 px-4 py-3 hover:bg-muted/40 transition-colors">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 mb-1">
          <Link
            to={`/projects/${projectId}/envs/${env.id}`}
            className="text-sm font-medium truncate hover:text-primary transition-colors"
          >
            {env.branch}
          </Link>
          <Badge variant="default">{env.kind}</Badge>
          <Badge variant={envStatusVariant(env.status)}>{env.status}</Badge>
        </div>
        {env.url && (
          <a
            href={`http://${env.url}`}
            target="_blank"
            rel="noreferrer"
            onClick={(e) => e.stopPropagation()}
            className="text-xs text-muted-foreground font-mono inline-flex items-center gap-1 hover:text-foreground transition-colors"
          >
            {env.url} <ExternalLink className="h-3 w-3" />
          </a>
        )}
      </div>
      <div className="flex gap-2 shrink-0">
        <Button variant="ghost" size="sm" className="text-xs h-7" onClick={onShowLog}>
          Build log
        </Button>
        <Button
          size="sm"
          className="text-xs h-7"
          onClick={onBuild}
          disabled={triggering || env.status === 'building'}
        >
          <Play className="h-3 w-3 mr-1" />
          {triggering ? 'Starting…' : env.status === 'building' ? 'Building…' : 'Build'}
        </Button>
      </div>
    </div>
  )
}
