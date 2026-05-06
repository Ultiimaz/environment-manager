import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { useState } from 'react'
import { ArrowLeft, ExternalLink, Play } from 'lucide-react'
import {
  getProject,
  listBuildsForEnv,
  triggerBuild,
  envRuntimeLogWsUrl,
  type Environment,
  type Build,
} from '@/services/api'
import { Section } from '@/components/ui/section'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { RuntimeLogViewer } from '@/components/runtime-log-viewer'
import { BuildLogViewer } from '@/components/projects/build-log-viewer'

function envStatusVariant(status: string): 'success' | 'failed' | 'pending' | 'default' {
  switch (status) {
    case 'running': return 'success'
    case 'failed': return 'failed'
    case 'building': return 'pending'
    default: return 'default'
  }
}

function buildStatusVariant(status: string): 'success' | 'failed' | 'pending' | 'default' {
  switch (status) {
    case 'success': return 'success'
    case 'failed':
    case 'cancelled': return 'failed'
    case 'running': return 'pending'
    default: return 'default'
  }
}

function relativeTime(iso: string): string {
  const dt = new Date(iso).getTime()
  const now = Date.now()
  const sec = Math.floor((now - dt) / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  return `${Math.floor(sec / 86400)}d ago`
}

export default function EnvDetail() {
  const { pid = '', envId = '' } = useParams<{ pid: string; envId: string }>()
  const [tailingLatest, setTailingLatest] = useState(true)

  const project = useQuery({
    queryKey: ['project', pid],
    queryFn: () => getProject(pid),
    enabled: !!pid,
  })

  const builds = useQuery({
    queryKey: ['builds', envId],
    queryFn: () => listBuildsForEnv(envId),
    enabled: !!envId,
    refetchInterval: 10000,
  })

  const env: Environment | undefined = project.data?.environments.find((e) => e.id === envId)

  async function onBuild() {
    try {
      await triggerBuild(envId)
      await Promise.all([project.refetch(), builds.refetch()])
      setTailingLatest(true)
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'Trigger failed'
      alert(msg)
    }
  }

  if (project.isLoading) return <div className="p-6 text-sm text-muted-foreground">Loading…</div>
  if (project.error) return <div className="p-6 text-sm text-red-400">{(project.error as Error).message}</div>
  if (!env) {
    return (
      <div className="p-6 space-y-4 max-w-3xl">
        <Link to={`/projects/${pid}`} className="inline-flex items-center text-xs text-muted-foreground hover:text-foreground transition-colors">
          <ArrowLeft className="h-3 w-3 mr-1" /> Project
        </Link>
        <Section className="border-red-900/60">
          <div className="text-xs text-red-400">env not found in project</div>
        </Section>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-4 max-w-5xl">
      <Link to={`/projects/${pid}`} className="inline-flex items-center text-xs text-muted-foreground hover:text-foreground transition-colors">
        <ArrowLeft className="h-3 w-3 mr-1" /> {project.data?.project.name}
      </Link>

      <header className="flex items-start justify-between">
        <div className="min-w-0">
          <div className="flex items-center gap-2 mb-1">
            <h1 className="text-xl font-semibold font-mono truncate">{env.branch}</h1>
            <Badge variant="default">{env.kind}</Badge>
            <Badge variant={envStatusVariant(env.status)}>{env.status}</Badge>
          </div>
          {env.url && (
            <a
              href={`http://${env.url}`}
              target="_blank"
              rel="noreferrer"
              className="text-xs text-muted-foreground font-mono inline-flex items-center gap-1 hover:text-foreground transition-colors"
            >
              {env.url} <ExternalLink className="h-3 w-3" />
            </a>
          )}
        </div>
        <Button size="sm" onClick={onBuild} disabled={env.status === 'building'}>
          <Play className="h-3 w-3 mr-1" />
          {env.status === 'building' ? 'Building…' : 'Build'}
        </Button>
      </header>

      <Section title="Live runtime logs">
        <RuntimeLogViewer url={envRuntimeLogWsUrl(envId)} height="h-[40vh]" />
      </Section>

      <Section
        title="Builds"
        action={
          <span className="text-xs text-muted-foreground">
            last {builds.data?.length ?? 0}
          </span>
        }
        flush
      >
        {builds.isLoading && <div className="text-xs text-muted-foreground p-4">loading…</div>}
        {builds.error && (
          <div className="text-xs text-red-400 p-4">{(builds.error as Error).message}</div>
        )}
        {builds.data && builds.data.length === 0 && (
          <div className="text-xs text-muted-foreground p-4">no builds yet</div>
        )}
        {builds.data && builds.data.length > 0 && (
          <BuildsTable
            builds={builds.data}
            envId={envId}
            tailingLatest={tailingLatest}
            onSelectLatest={() => setTailingLatest(true)}
          />
        )}
      </Section>

      {builds.data && builds.data.length > 0 && tailingLatest && (
        <Section
          title={
            <span className="font-mono text-xs">
              Build log · {builds.data[0].id.slice(0, 8)} · {builds.data[0].status}
            </span>
          }
          action={
            <Button variant="ghost" size="sm" onClick={() => setTailingLatest(false)}>
              Close
            </Button>
          }
        >
          <BuildLogViewer envId={envId} />
        </Section>
      )}
    </div>
  )
}

interface BuildsTableProps {
  builds: Build[]
  envId: string
  tailingLatest: boolean
  onSelectLatest: () => void
}

function BuildsTable({ builds, tailingLatest, onSelectLatest }: BuildsTableProps) {
  return (
    <table className="w-full text-xs">
      <thead className="text-[11px] text-muted-foreground uppercase tracking-wider">
        <tr className="border-b border-border">
          <th className="text-left py-2.5 px-4 font-medium">Build</th>
          <th className="text-left py-2.5 px-1 font-medium">SHA</th>
          <th className="text-left py-2.5 px-1 font-medium">Status</th>
          <th className="text-left py-2.5 px-1 font-medium">Trigger</th>
          <th className="text-left py-2.5 px-1 font-medium tabular-nums">Started</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {builds.map((b, i) => {
          const isLatest = i === 0
          return (
            <tr key={b.id} className="border-b border-border last:border-0 hover:bg-muted/40">
              <td className="py-2.5 px-4 font-mono">{b.id.slice(0, 8)}</td>
              <td className="py-2.5 px-1 font-mono text-muted-foreground">
                {b.sha?.slice(0, 7) || '—'}
              </td>
              <td className="py-2.5 px-1">
                <Badge variant={buildStatusVariant(b.status)}>{b.status}</Badge>
              </td>
              <td className="py-2.5 px-1 text-muted-foreground">{b.triggered_by}</td>
              <td className="py-2.5 px-1 text-muted-foreground tabular-nums">{relativeTime(b.started_at)}</td>
              <td className="py-2.5 px-1 text-right pr-4">
                {isLatest ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-xs h-7"
                    onClick={onSelectLatest}
                    disabled={tailingLatest}
                  >
                    {tailingLatest ? 'Tailing' : 'Tail logs'}
                  </Button>
                ) : (
                  <Link
                    to={`#`}
                    onClick={(e) => {
                      e.preventDefault()
                      window.open(`/api/v1/builds/${b.id}/log`, '_blank')
                    }}
                    className="text-xs text-muted-foreground hover:text-foreground"
                  >
                    open log
                  </Link>
                )}
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}
