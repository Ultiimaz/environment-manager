import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { GitCommit, PlayCircle, ChevronDown, Download } from 'lucide-react'
import { listProjects, getProject, listBuildsForEnv, getBuildLog, type Build } from '@/services/api'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

// Stitch Builds (.design/stitch-v1/05-builds.html)
// Cross-project build history: status filter pills, project/trigger dropdowns,
// live-tail toggle, dense table with hover-revealed actions.

interface EnrichedBuild extends Build {
  project_id: string
  project_name: string
  branch: string
  env_kind: string
}

async function fetchAllBuilds(): Promise<EnrichedBuild[]> {
  const projects = await listProjects()
  const all: EnrichedBuild[] = []
  for (const p of projects) {
    const detail = await getProject(p.id)
    for (const env of detail.environments) {
      try {
        const builds = await listBuildsForEnv(env.id)
        for (const b of builds) {
          all.push({
            ...b,
            project_id: p.id,
            project_name: p.name,
            branch: env.branch,
            env_kind: env.kind,
          })
        }
      } catch {
        // skip — env without builds
      }
    }
  }
  all.sort((a, b) => (a.started_at < b.started_at ? 1 : -1))
  return all
}

const STATUS_TOKENS = {
  running: { dot: 'bg-warning', text: 'text-warning', bg: 'bg-warning/10', border: 'border-warning/20', pulse: true, accent: 'border-l-warning' },
  building: { dot: 'bg-warning', text: 'text-warning', bg: 'bg-warning/10', border: 'border-warning/20', pulse: true, accent: 'border-l-warning' },
  success: { dot: 'bg-primary', text: 'text-primary', bg: 'bg-primary/10', border: 'border-primary/20', pulse: false, accent: '' },
  failed: { dot: 'bg-destructive', text: 'text-destructive', bg: 'bg-destructive/10', border: 'border-destructive/20', pulse: false, accent: 'border-l-destructive' },
  cancelled: { dot: 'bg-muted-foreground', text: 'text-muted-foreground', bg: 'bg-secondary', border: 'border-border', pulse: false, accent: '' },
  default: { dot: 'bg-muted-foreground', text: 'text-muted-foreground', bg: 'bg-secondary', border: 'border-border', pulse: false, accent: '' },
} as const

function StatusPill({ status }: { status: string }) {
  const tok = (STATUS_TOKENS as Record<string, (typeof STATUS_TOKENS)['default']>)[status] ?? STATUS_TOKENS.default
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10px] uppercase tracking-wider font-semibold',
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

function relativeTime(iso: string): string {
  const dt = new Date(iso).getTime()
  const now = Date.now()
  const sec = Math.floor((now - dt) / 1000)
  if (sec < 5) return 'just now'
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

function isLegacyLogPath(path?: string): boolean {
  return !!path && path.endsWith('/latest.log')
}

interface LogPanelProps {
  build: EnrichedBuild
  onClose: () => void
}

function LogPanel({ build, onClose }: LogPanelProps) {
  const legacy = isLegacyLogPath(build.log_path)
  const { data, isLoading, error } = useQuery({
    queryKey: ['build-log', build.id],
    queryFn: () => getBuildLog(build.id),
    enabled: !legacy,
  })
  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3 border-b border-border">
        <div className="flex items-center gap-2 min-w-0">
          <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse" />
          <h2 className="text-[12px] font-semibold uppercase tracking-wider truncate">
            Build log
          </h2>
          <span className="font-mono text-[11px] text-muted-foreground truncate">
            {build.project_name} / {build.branch} · {build.id.slice(0, 8)} · {build.status}
          </span>
        </div>
        <Button variant="ghost" size="sm" className="h-7 text-[11px] shrink-0" onClick={onClose}>
          Close
        </Button>
      </div>
      <div className="p-4">
        {legacy && (
          <div className="text-[12px] text-muted-foreground">
            Log not retained — this build predates per-build log retention.
          </div>
        )}
        {!legacy && isLoading && <div className="text-[12px] text-muted-foreground">loading log…</div>}
        {!legacy && error && (
          <div className="text-[12px] text-destructive">{(error as Error).message}</div>
        )}
        {!legacy && data !== undefined && (
          <pre className="text-[12px] font-mono whitespace-pre-wrap bg-background border border-border p-3 rounded max-h-[60vh] overflow-y-auto">
            {data || '(empty)'}
          </pre>
        )}
      </div>
    </div>
  )
}

type StatusFilter = 'all' | 'running' | 'success' | 'failed'

export default function Builds() {
  const [liveTail, setLiveTail] = useState(true)
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [projectFilter, setProjectFilter] = useState<string>('all')
  const [selected, setSelected] = useState<EnrichedBuild | null>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['builds', 'all'],
    queryFn: fetchAllBuilds,
    refetchInterval: liveTail ? 5000 : false,
  })

  const projectsInList = useMemo(() => {
    if (!data) return []
    const seen = new Map<string, string>()
    for (const b of data) {
      if (!seen.has(b.project_id)) seen.set(b.project_id, b.project_name)
    }
    return Array.from(seen, ([id, name]) => ({ id, name }))
  }, [data])

  const filtered = useMemo(() => {
    if (!data) return []
    return data.filter((b) => {
      if (statusFilter !== 'all') {
        if (statusFilter === 'running' && !['running', 'building'].includes(b.status)) return false
        if (statusFilter === 'success' && b.status !== 'success') return false
        if (statusFilter === 'failed' && !['failed', 'cancelled'].includes(b.status)) return false
      }
      if (projectFilter !== 'all' && b.project_id !== projectFilter) return false
      return true
    })
  }, [data, statusFilter, projectFilter])

  const totalCount = data?.length ?? 0
  const shownCount = filtered.length

  return (
    <div className="px-6 py-6 space-y-6 max-w-[1400px] mx-auto">
      {/* Header */}
      <header>
        <h1 className="text-[28px] font-semibold tracking-tight leading-none">Builds</h1>
        <p className="mt-1.5 text-sm text-muted-foreground">
          All builds across all projects, latest first.
        </p>
      </header>

      {/* Filter row */}
      <div className="flex items-center justify-between gap-3 flex-wrap pb-4 border-b border-border">
        <div className="flex items-center gap-3 flex-wrap">
          {/* Status pills */}
          <div className="inline-flex bg-card border border-border rounded p-0.5">
            {(['all', 'running', 'success', 'failed'] as StatusFilter[]).map((s) => (
              <button
                key={s}
                onClick={() => setStatusFilter(s)}
                className={cn(
                  'px-3 py-1 text-[12px] font-medium rounded transition-colors capitalize',
                  statusFilter === s
                    ? 'bg-secondary text-foreground'
                    : 'text-muted-foreground hover:text-foreground'
                )}
              >
                {s}
              </button>
            ))}
          </div>

          {/* Project dropdown */}
          <div className="relative">
            <select
              value={projectFilter}
              onChange={(e) => setProjectFilter(e.target.value)}
              className="appearance-none bg-card border border-border text-muted-foreground text-[12px] rounded pl-3 pr-8 py-1.5 focus:outline-none focus:border-muted-foreground/40 hover:text-foreground transition-colors"
            >
              <option value="all">Project: All</option>
              {projectsInList.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            <ChevronDown className="absolute right-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground pointer-events-none" />
          </div>
        </div>

        <div className="flex items-center gap-3">
          <label className="flex items-center gap-2 cursor-pointer select-none">
            <span className="text-[12px] text-muted-foreground">Live tail</span>
            <button
              type="button"
              role="switch"
              aria-checked={liveTail}
              onClick={() => setLiveTail((v) => !v)}
              className={cn(
                'relative h-4 w-8 rounded-full transition-colors',
                liveTail ? 'bg-primary' : 'bg-secondary border border-border'
              )}
            >
              <span
                className={cn(
                  'absolute top-0.5 h-3 w-3 rounded-full bg-background transition-transform',
                  liveTail ? 'translate-x-[18px]' : 'translate-x-0.5'
                )}
              />
            </button>
          </label>
          <span className="h-4 border-l border-border" />
          <button className="inline-flex items-center gap-1.5 text-[12px] font-medium text-muted-foreground hover:text-foreground transition-colors">
            <Download className="h-3.5 w-3.5" />
            Export
          </button>
        </div>
      </div>

      {/* Table */}
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        {isLoading && (
          <div className="px-4 py-6 text-[12px] text-muted-foreground">loading…</div>
        )}
        {error && (
          <div className="px-4 py-6 text-[12px] text-destructive">{(error as Error).message}</div>
        )}
        {data && filtered.length === 0 && (
          <div className="px-4 py-6 text-[12px] text-muted-foreground">
            {totalCount === 0 ? 'No builds yet.' : 'No builds match the current filters.'}
          </div>
        )}
        {filtered.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-left">
              <thead className="bg-background/40 border-b border-border">
                <tr>
                  <th className="py-2.5 px-4 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider w-[110px]">Status</th>
                  <th className="py-2.5 px-4 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider">Build</th>
                  <th className="py-2.5 px-4 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider">Project</th>
                  <th className="py-2.5 px-4 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider">Env</th>
                  <th className="py-2.5 px-4 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider">Trigger</th>
                  <th className="py-2.5 px-4 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider text-right">Started</th>
                  <th className="py-2.5 px-4 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider text-right">Duration</th>
                  <th className="py-2.5 px-4 text-[10px] font-medium text-muted-foreground/80 uppercase tracking-wider text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {filtered.slice(0, 100).map((b) => {
                  const tok = (STATUS_TOKENS as Record<string, (typeof STATUS_TOKENS)['default']>)[b.status] ?? STATUS_TOKENS.default
                  const isManual = b.triggered_by !== 'webhook' && b.triggered_by !== 'github-actions'
                  return (
                    <tr
                      key={`${b.env_id}-${b.id}`}
                      className={cn(
                        'border-b border-border last:border-0 hover:bg-secondary/40 transition-colors group border-l-2 border-l-transparent',
                        tok.accent && 'border-l-2',
                        tok.accent
                      )}
                    >
                      <td className="py-3 px-4">
                        <StatusPill status={b.status} />
                      </td>
                      <td className="py-3 px-4 font-mono text-[12px] text-muted-foreground">
                        {b.id.slice(0, 8)}
                      </td>
                      <td className="py-3 px-4">
                        <Link
                          to={`/projects/${b.project_id}`}
                          className="text-[13px] font-medium hover:text-primary transition-colors"
                        >
                          {b.project_name}
                        </Link>
                      </td>
                      <td className="py-3 px-4 text-[12px] text-muted-foreground font-mono">
                        {b.env_kind}
                      </td>
                      <td className="py-3 px-4">
                        <div className="flex items-center gap-1.5 text-muted-foreground">
                          {isManual ? (
                            <PlayCircle className="h-3.5 w-3.5" />
                          ) : (
                            <GitCommit className="h-3.5 w-3.5" />
                          )}
                          <span className="font-mono text-[12px]">{b.triggered_by}</span>
                        </div>
                      </td>
                      <td className="py-3 px-4 font-mono text-[12px] text-muted-foreground tabular-nums text-right">
                        {relativeTime(b.started_at)}
                      </td>
                      <td className="py-3 px-4 font-mono text-[12px] text-muted-foreground tabular-nums text-right">
                        {buildDuration(b)}
                      </td>
                      <td className="py-3 px-4 text-right">
                        <div className="flex items-center justify-end gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                          <button
                            onClick={() => setSelected(b)}
                            className="text-[11px] text-muted-foreground hover:text-foreground px-2 py-1 rounded hover:bg-secondary transition-colors"
                          >
                            Logs
                          </button>
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between text-[12px] text-muted-foreground">
        <span>
          Showing {Math.min(shownCount, 100)} of {totalCount} builds
        </span>
      </div>

      {/* Log drawer */}
      {selected && <LogPanel build={selected} onClose={() => setSelected(null)} />}
    </div>
  )
}
