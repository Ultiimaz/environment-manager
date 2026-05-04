import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, RefreshCw, Play, ExternalLink } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { getProject, triggerBuild } from '@/services/api'
import type { ProjectDetail, Environment } from '@/types'
import { BuildLogViewer } from '@/components/projects/build-log-viewer'

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

  if (error) return <div className="p-6 text-destructive">{error}</div>
  if (!data) return <div className="p-6">Loading...</div>

  const { project, environments } = data
  const sortedEnvs = [...environments].sort((a, b) => {
    if (a.kind === 'prod' && b.kind !== 'prod') return -1
    if (b.kind === 'prod' && a.kind !== 'prod') return 1
    return a.branch_slug.localeCompare(b.branch_slug)
  })

  return (
    <div className="p-6">
      <Link to="/projects" className="inline-flex items-center text-sm text-muted-foreground mb-4">
        <ArrowLeft className="h-4 w-4 mr-1" /> Projects
      </Link>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold">{project.name}</h1>
          <p className="text-muted-foreground text-sm">{project.repo_url}</p>
          <p className="text-muted-foreground text-sm">default: {project.default_branch}</p>
        </div>
        <Button variant="outline" onClick={load}>
          <RefreshCw className="h-4 w-4 mr-2" /> Refresh
        </Button>
      </div>

      <h2 className="text-lg font-semibold mb-3">Environments</h2>
      <div className="grid gap-3 mb-6">
        {sortedEnvs.map(e => (
          <EnvCard
            key={e.id}
            env={e}
            triggering={triggering === e.id}
            onBuild={() => onBuild(e.id)}
            onShowLog={() => setLogEnvId(e.id)}
          />
        ))}
      </div>

      {logEnvId && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center justify-between">
              <span>Build log: {logEnvId}</span>
              <Button variant="ghost" size="sm" onClick={() => setLogEnvId(null)}>
                Close
              </Button>
            </CardTitle>
          </CardHeader>
          <CardContent>
            <BuildLogViewer envId={logEnvId} />
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function EnvCard({
  env, triggering, onBuild, onShowLog,
}: {
  env: Environment
  triggering: boolean
  onBuild: () => void
  onShowLog: () => void
}) {
  const statusColor = {
    pending: 'text-muted-foreground',
    building: 'text-blue-500',
    running: 'text-green-500',
    failed: 'text-destructive',
    destroying: 'text-orange-500',
  }[env.status] || 'text-muted-foreground'

  return (
    <Card>
      <CardContent className="pt-6">
        <div className="flex items-center justify-between gap-4">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-1">
              <span className="font-medium truncate">{env.branch}</span>
              <span className="text-xs px-2 py-0.5 rounded bg-secondary">{env.kind}</span>
              <span className={`text-xs ${statusColor}`}>{env.status}</span>
            </div>
            {env.url && (
              <a
                href={`http://${env.url}`}
                target="_blank"
                rel="noreferrer"
                className="text-sm text-primary inline-flex items-center gap-1"
              >
                {env.url} <ExternalLink className="h-3 w-3" />
              </a>
            )}
          </div>
          <div className="flex gap-2 shrink-0">
            <Button variant="outline" size="sm" onClick={onShowLog}>
              Logs
            </Button>
            <Button size="sm" onClick={onBuild} disabled={triggering || env.status === 'building'}>
              <Play className="h-3 w-3 mr-1" />
              {triggering ? 'Starting...' : env.status === 'building' ? 'Building...' : 'Build'}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
