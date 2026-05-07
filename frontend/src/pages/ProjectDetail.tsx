import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { RefreshCw, Play, ExternalLink } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { getProject, triggerBuild } from '@/services/api'
import type { ProjectDetail, Environment } from '@/services/api'
import { BuildLogViewer } from '@/components/projects/build-log-viewer'
import { cn } from '@/lib/utils'

// Stitch Project Detail (.design/stitch-v1/03-project-detail.html)
// Single project view with metric tiles, env grid (3-col), tabs, runtime log drawer.

const STATUS_TOKENS = {
  running: { dot: 'bg-primary', text: 'text-primary', bg: 'bg-primary/10', border: 'border-primary/30' },
  building: { dot: 'bg-warning', text: 'text-warning', bg: 'bg-warning/10', border: 'border-warning/30' },
  failed: { dot: 'bg-destructive', text: 'text-destructive', bg: 'bg-destructive/10', border: 'border-destructive/30' },
  default: { dot: 'bg-muted-foreground', text: 'text-muted-foreground', bg: 'bg-secondary', border: 'border-border' },
} as const

const STATUS_LOOKUP = STATUS_TOKENS as unknown as Record<string, typeof STATUS_TOKENS.default>

function StatusPill({ status }: { status: string }) {
  const tok = STATUS_LOOKUP[status] ?? STATUS_TOKENS.default
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

function MetricTile({ label, value, hint }: { label: string; value: string | number; hint?: string }) {
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground/80">
        {label}
      </div>
      <div className="mt-2 font-mono text-2xl font-medium tabular-nums">{value}</div>
      {hint && <div className="mt-1 text-[11px] text-muted-foreground/60">{hint}</div>}
    </div>
  )
}

function EnvCard({
  env,
  projectId,
  triggering,
  onBuild,
  onShowLog,
}: {
  env: Environment
  projectId: string
  triggering: boolean
  onBuild: () => void
  onShowLog: () => void
}) {
  return (
    <div className="rounded-lg border border-border bg-card p-4 hover:bg-secondary/40 transition-colors">
      <div className="flex items-center justify-between gap-2 mb-2">
        <Link
          to={`/projects/${projectId}/envs/${env.id}`}
          className="font-mono text-[14px] font-semibold truncate hover:text-primary transition-colors"
        >
          {env.branch}
        </Link>
        <span className="inline-flex items-center rounded-full border border-border bg-secondary px-2 py-0.5 text-[10px] uppercase tracking-wider text-muted-foreground shrink-0">
          {env.kind}
        </span>
      </div>
      <div className="mb-3">
        <StatusPill status={env.status} />
      </div>
      {env.url && (
        <a
          href={`http://${env.url}`}
          target="_blank"
          rel="noreferrer"
          onClick={(e) => e.stopPropagation()}
          className="text-[11px] text-muted-foreground font-mono inline-flex items-center gap-1 hover:text-foreground transition-colors mb-3"
        >
          {env.url} <ExternalLink className="h-3 w-3" />
        </a>
      )}
      {env.last_deployed_sha && (
        <div className="text-[11px] text-muted-foreground/70 font-mono mb-3 truncate">
          last sha: {env.last_deployed_sha.slice(0, 7)}
        </div>
      )}
      <div className="flex gap-2 mt-3 pt-3 border-t border-border">
        <Button variant="ghost" size="sm" className="text-[11px] h-7 flex-1" onClick={onShowLog}>
          Build log
        </Button>
        <Button
          size="sm"
          className="text-[11px] h-7 flex-1"
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

export default function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>()
  const [data, setData] = useState<ProjectDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [logEnvId, setLogEnvId] = useState<string | null>(null)
  const [triggering, setTriggering] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState<'environments' | 'builds'>('environments')

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

  useEffect(() => {
    load()
  }, [id])

  useEffect(() => {
    if (!data) return
    const anyBuilding = data.environments.some((e) => e.status === 'building')
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
      toast.error('Build trigger failed', { description: msg })
    } finally {
      setTriggering(null)
    }
  }

  if (error)
    return (
      <div className="px-6 py-6">
        <div className="rounded-lg border border-destructive/40 bg-destructive/10 p-4 text-[12px] text-destructive">
          {error}
        </div>
      </div>
    )
  if (!data)
    return <div className="px-6 py-6 text-[12px] text-muted-foreground">Loading…</div>

  const { project, environments } = data
  const sortedEnvs = [...environments].sort((a, b) => {
    if (a.kind === 'prod' && b.kind !== 'prod') return -1
    if (b.kind === 'prod' && a.kind !== 'prod') return 1
    return a.branch_slug.localeCompare(b.branch_slug)
  })

  return (
    <div className="px-6 py-6 space-y-6 max-w-[1400px] mx-auto">
      {/* Header */}
      <header className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-[28px] font-semibold tracking-tight leading-none truncate">
            {project.name}
          </h1>
          <div className="mt-2 flex items-center gap-3 flex-wrap text-[12px] text-muted-foreground">
            <a
              href={project.repo_url}
              target="_blank"
              rel="noreferrer"
              className="font-mono hover:text-foreground transition-colors inline-flex items-center gap-1"
            >
              {project.repo_url} <ExternalLink className="h-3 w-3" />
            </a>
            <span className="text-muted-foreground/40">·</span>
            <span className="font-mono">default: {project.default_branch}</span>
            {project.external_domain && (
              <>
                <span className="text-muted-foreground/40">·</span>
                <a
                  href={`http://${project.external_domain}`}
                  target="_blank"
                  rel="noreferrer"
                  className="font-mono hover:text-foreground transition-colors inline-flex items-center gap-1"
                >
                  {project.external_domain} <ExternalLink className="h-3 w-3" />
                </a>
              </>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <Button variant="outline" size="sm" onClick={load} className="h-8">
            <RefreshCw className="h-3.5 w-3.5 mr-1.5" /> Refresh
          </Button>
        </div>
      </header>

      {/* Metric tiles */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <MetricTile label="Environments" value={environments.length} />
        <MetricTile
          label="Active"
          value={environments.filter((e) => e.status === 'running').length}
        />
        <MetricTile
          label="Building"
          value={environments.filter((e) => e.status === 'building').length}
        />
        <MetricTile
          label="Failed"
          value={environments.filter((e) => e.status === 'failed').length}
        />
      </div>

      {/* Tabs */}
      <div className="border-b border-border">
        <nav className="flex gap-1">
          {(['environments', 'builds'] as const).map((tab) => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              className={cn(
                'relative px-3 py-2 text-[12px] font-medium uppercase tracking-wider transition-colors',
                activeTab === tab
                  ? 'text-foreground'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              {tab}
              {activeTab === tab && (
                <span className="absolute left-0 right-0 bottom-[-1px] h-[2px] bg-primary" />
              )}
            </button>
          ))}
        </nav>
      </div>

      {/* Active tab content */}
      {activeTab === 'environments' && (
        <div>
          <div className="mb-3 text-[10px] font-medium uppercase tracking-wider text-muted-foreground/80">
            Active environments ({sortedEnvs.length})
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
            {sortedEnvs.map((e) => (
              <EnvCard
                key={e.id}
                env={e}
                projectId={project.id}
                triggering={triggering === e.id}
                onBuild={() => onBuild(e.id)}
                onShowLog={() => setLogEnvId(e.id)}
              />
            ))}
          </div>
        </div>
      )}

      {activeTab === 'builds' && (
        <div className="rounded-lg border border-border bg-card p-6 text-[12px] text-muted-foreground">
          See <Link to="/builds" className="text-primary hover:underline">/builds</Link> for the cross-project view.
        </div>
      )}

      {/* Build log drawer */}
      {logEnvId && (
        <div className="rounded-lg border border-border bg-card overflow-hidden">
          <div className="flex items-center justify-between px-4 py-3 border-b border-border">
            <div className="flex items-center gap-2">
              <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse" />
              <h2 className="text-[12px] font-semibold uppercase tracking-wider">
                Build log
              </h2>
              <span className="font-mono text-[11px] text-muted-foreground">{logEnvId}</span>
            </div>
            <Button variant="ghost" size="sm" className="h-7 text-[11px]" onClick={() => setLogEnvId(null)}>
              Close
            </Button>
          </div>
          <div className="p-4">
            <BuildLogViewer envId={logEnvId} />
          </div>
        </div>
      )}
    </div>
  )
}
