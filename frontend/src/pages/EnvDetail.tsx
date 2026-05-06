import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { useState } from 'react'
import { ExternalLink, Play, CheckCircle2, XCircle, Loader2, Circle } from 'lucide-react'
import {
  getProject,
  listBuildsForEnv,
  triggerBuild,
  envRuntimeLogWsUrl,
  type Environment,
  type Build,
} from '@/services/api'
import { Button } from '@/components/ui/button'
import { RuntimeLogViewer } from '@/components/runtime-log-viewer'
import { BuildLogViewer } from '@/components/projects/build-log-viewer'
import { cn } from '@/lib/utils'

// Stitch Environment Detail (.design/stitch-v1/04-environment-detail.html)
// Two-column layout: live runtime logs (7/12) + side panels (5/12), then builds table.

const STATUS_TOKENS = {
  running: { dot: 'bg-primary', text: 'text-primary', bg: 'bg-primary/10', border: 'border-primary/30', pulse: true },
  building: { dot: 'bg-warning', text: 'text-warning', bg: 'bg-warning/10', border: 'border-warning/30', pulse: true },
  failed: { dot: 'bg-destructive', text: 'text-destructive', bg: 'bg-destructive/10', border: 'border-destructive/30', pulse: false },
  success: { dot: 'bg-primary', text: 'text-primary', bg: 'bg-primary/10', border: 'border-primary/30', pulse: false },
  cancelled: { dot: 'bg-muted-foreground', text: 'text-muted-foreground', bg: 'bg-secondary', border: 'border-border', pulse: false },
  default: { dot: 'bg-muted-foreground', text: 'text-muted-foreground', bg: 'bg-secondary', border: 'border-border', pulse: false },
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
      <span className={cn('h-1.5 w-1.5 rounded-full', tok.dot, tok.pulse && 'animate-pulse')} />
      {status}
    </span>
  )
}

function isLegacyLogPath(path?: string): boolean {
  return !!path && path.endsWith('/latest.log')
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

function buildDuration(b: Build): string {
  if (!b.finished_at) return '—'
  const ms = new Date(b.finished_at).getTime() - new Date(b.started_at).getTime()
  if (ms < 1000) return `${ms}ms`
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  return `${m}m ${s % 60}s`
}

function BuildStatusIcon({ status }: { status: string }) {
  const cls = 'h-4 w-4'
  switch (status) {
    case 'success': return <CheckCircle2 className={cn(cls, 'text-primary')} />
    case 'failed':
    case 'cancelled': return <XCircle className={cn(cls, 'text-destructive')} />
    case 'running': return <Loader2 className={cn(cls, 'text-warning animate-spin')} />
    default: return <Circle className={cn(cls, 'text-muted-foreground')} />
  }
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

  if (project.isLoading)
    return <div className="px-6 py-6 text-[12px] text-muted-foreground">Loading…</div>
  if (project.error)
    return (
      <div className="px-6 py-6">
        <div className="rounded-lg border border-destructive/40 bg-destructive/10 p-4 text-[12px] text-destructive">
          {(project.error as Error).message}
        </div>
      </div>
    )
  if (!env)
    return (
      <div className="px-6 py-6">
        <div className="rounded-lg border border-destructive/40 bg-destructive/10 p-4 text-[12px] text-destructive">
          Environment not found in this project.{' '}
          <Link to={`/projects/${pid}`} className="underline">Back to project</Link>
        </div>
      </div>
    )

  const latestBuild = builds.data?.[0]

  return (
    <div className="px-6 py-6 space-y-6 max-w-[1400px] mx-auto">
      {/* Header */}
      <header className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-3 flex-wrap">
            <h1 className="text-[28px] font-semibold tracking-tight leading-none truncate font-mono">
              {env.branch}
            </h1>
            <span className="inline-flex items-center rounded-full border border-border bg-secondary px-2 py-0.5 text-[10px] uppercase tracking-wider text-muted-foreground">
              {env.kind}
            </span>
            <StatusPill status={env.status} />
          </div>
          <div className="mt-2 flex items-center gap-3 flex-wrap text-[12px] text-muted-foreground">
            {env.url && (
              <a
                href={`http://${env.url}`}
                target="_blank"
                rel="noreferrer"
                className="font-mono hover:text-foreground transition-colors inline-flex items-center gap-1"
              >
                {env.url} <ExternalLink className="h-3 w-3" />
              </a>
            )}
            <span className="text-muted-foreground/40">·</span>
            <span className="font-mono text-muted-foreground/80">id: {env.id}</span>
          </div>
        </div>
        <Button size="sm" className="h-8 shrink-0" onClick={onBuild} disabled={env.status === 'building'}>
          <Play className="h-3.5 w-3.5 mr-1.5" />
          {env.status === 'building' ? 'Building…' : 'Build'}
        </Button>
      </header>

      {/* Two-column: logs + side panels */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-4">
        {/* Live runtime logs */}
        <div className="lg:col-span-7 xl:col-span-8">
          <div className="rounded-lg border border-border bg-card overflow-hidden">
            <div className="flex items-center justify-between px-4 h-10 border-b border-border bg-background/40">
              <div className="flex items-center gap-2">
                <span className="text-[12px] font-semibold uppercase tracking-wider">
                  Live runtime logs
                </span>
                <span className="inline-flex items-center gap-1.5 rounded border border-primary/20 bg-primary/10 px-1.5 py-0.5 text-[10px] font-mono text-primary">
                  <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse" />
                  ws · open
                </span>
              </div>
            </div>
            <RuntimeLogViewer url={envRuntimeLogWsUrl(envId)} height="h-[420px]" />
          </div>
        </div>

        {/* Side panels */}
        <div className="lg:col-span-5 xl:col-span-4 flex flex-col gap-4">
          {/* Environment info */}
          <div className="rounded-lg border border-border bg-card p-4">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-[11px] font-semibold uppercase tracking-wider">Environment</h3>
            </div>
            <dl className="space-y-0 text-[12px]">
              <Row label="branch" value={env.branch} />
              <Row label="slug" value={env.branch_slug} />
              <Row label="kind" value={env.kind} />
              {env.url && <Row label="url" value={env.url} />}
              {env.last_deployed_sha && (
                <Row label="deployed" value={env.last_deployed_sha.slice(0, 7)} last />
              )}
            </dl>
          </div>

          {/* Active build */}
          <div className="rounded-lg border border-border bg-card p-4">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-[11px] font-semibold uppercase tracking-wider">Active build</h3>
              {latestBuild && <StatusPill status={latestBuild.status} />}
            </div>
            {!latestBuild && (
              <div className="text-[12px] text-muted-foreground">No builds yet.</div>
            )}
            {latestBuild && (
              <>
                <div className="flex items-center gap-3 mb-3">
                  <div
                    className={cn(
                      'h-10 w-10 rounded flex items-center justify-center border',
                      latestBuild.status === 'success' && 'bg-primary/10 border-primary/20',
                      latestBuild.status === 'failed' && 'bg-destructive/10 border-destructive/20',
                      latestBuild.status === 'running' && 'bg-warning/10 border-warning/20',
                      !['success', 'failed', 'running'].includes(latestBuild.status) && 'bg-secondary border-border'
                    )}
                  >
                    <BuildStatusIcon status={latestBuild.status} />
                  </div>
                  <div className="min-w-0">
                    <div className="font-mono text-[13px] flex items-center gap-2">
                      {latestBuild.sha?.slice(0, 7) || latestBuild.id.slice(0, 7)}
                      <span className="text-[10px] bg-secondary border border-border px-1.5 py-0.5 rounded text-muted-foreground">
                        {env.branch}
                      </span>
                    </div>
                    <div className="text-[11px] text-muted-foreground mt-1">
                      {relativeTime(latestBuild.started_at)} · by {latestBuild.triggered_by}
                    </div>
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-4 border-t border-border pt-3">
                  <div>
                    <div className="text-[10px] uppercase tracking-wider text-muted-foreground/80 mb-1">
                      Duration
                    </div>
                    <div className="font-mono text-[13px]">{buildDuration(latestBuild)}</div>
                  </div>
                  <div>
                    <div className="text-[10px] uppercase tracking-wider text-muted-foreground/80 mb-1">
                      Status
                    </div>
                    <div className="font-mono text-[13px]">{latestBuild.status}</div>
                  </div>
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Builds table */}
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        <div className="flex items-center justify-between px-4 py-3 border-b border-border">
          <h3 className="text-[12px] font-semibold uppercase tracking-wider">
            Recent builds ({builds.data?.length ?? 0})
          </h3>
        </div>
        {builds.isLoading && (
          <div className="px-4 py-6 text-[12px] text-muted-foreground">loading…</div>
        )}
        {builds.error && (
          <div className="px-4 py-6 text-[12px] text-destructive">
            {(builds.error as Error).message}
          </div>
        )}
        {builds.data && builds.data.length === 0 && (
          <div className="px-4 py-6 text-[12px] text-muted-foreground">no builds yet</div>
        )}
        {builds.data && builds.data.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-left">
              <thead>
                <tr className="border-b border-border">
                  <th className="py-2.5 px-4 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider">Build</th>
                  <th className="py-2.5 px-2 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider">SHA</th>
                  <th className="py-2.5 px-2 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider">Status</th>
                  <th className="py-2.5 px-2 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider">Trigger</th>
                  <th className="py-2.5 px-2 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider">Started</th>
                  <th className="py-2.5 px-2 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider text-right">Duration</th>
                  <th className="py-2.5 px-4"></th>
                </tr>
              </thead>
              <tbody>
                {builds.data.map((b, i) => {
                  const isLatest = i === 0
                  const legacy = isLegacyLogPath(b.log_path)
                  return (
                    <tr key={b.id} className="border-b border-border last:border-0 hover:bg-secondary/40 transition-colors group">
                      <td className="py-2.5 px-4 font-mono text-[12px]">{b.id.slice(0, 8)}</td>
                      <td className="py-2.5 px-2 font-mono text-[12px] text-muted-foreground">
                        {b.sha?.slice(0, 7) || '—'}
                      </td>
                      <td className="py-2.5 px-2">
                        <StatusPill status={b.status} />
                      </td>
                      <td className="py-2.5 px-2 text-[12px] text-muted-foreground font-mono">{b.triggered_by}</td>
                      <td className="py-2.5 px-2 text-[12px] text-muted-foreground tabular-nums">{relativeTime(b.started_at)}</td>
                      <td className="py-2.5 px-2 text-[12px] text-muted-foreground tabular-nums text-right font-mono">{buildDuration(b)}</td>
                      <td className="py-2.5 px-4 text-right">
                        {isLatest ? (
                          <button
                            className="text-[11px] text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
                            onClick={() => setTailingLatest(true)}
                            disabled={tailingLatest}
                          >
                            {tailingLatest ? 'Tailing' : 'Tail logs'}
                          </button>
                        ) : legacy ? (
                          <span
                            className="text-[11px] text-muted-foreground/50"
                            title="Log not retained — predates per-build retention"
                          >
                            not retained
                          </span>
                        ) : (
                          <a
                            href={`/api/v1/builds/${b.id}/log`}
                            target="_blank"
                            rel="noreferrer"
                            className="text-[11px] text-muted-foreground hover:text-foreground transition-colors"
                          >
                            open log
                          </a>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Build log drawer */}
      {builds.data && builds.data.length > 0 && tailingLatest && (
        <div className="rounded-lg border border-border bg-card overflow-hidden">
          <div className="flex items-center justify-between px-4 py-3 border-b border-border">
            <div className="flex items-center gap-2">
              <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse" />
              <h2 className="text-[12px] font-semibold uppercase tracking-wider">
                Build log
              </h2>
              <span className="font-mono text-[11px] text-muted-foreground">
                {builds.data[0].id.slice(0, 8)} · {builds.data[0].status}
              </span>
            </div>
            <Button
              variant="ghost"
              size="sm"
              className="h-7 text-[11px]"
              onClick={() => setTailingLatest(false)}
            >
              Close
            </Button>
          </div>
          <div className="p-4">
            <BuildLogViewer envId={envId} />
          </div>
        </div>
      )}
    </div>
  )
}

function Row({ label, value, last }: { label: string; value: string; last?: boolean }) {
  return (
    <div
      className={cn(
        'flex justify-between items-center py-2 gap-3',
        !last && 'border-b border-border'
      )}
    >
      <dt className="font-mono text-[11px] text-muted-foreground uppercase tracking-wider">{label}</dt>
      <dd className="font-mono text-[12px] truncate">{value}</dd>
    </div>
  )
}
