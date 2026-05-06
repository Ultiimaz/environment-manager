import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { listProjects, getProject, listBuildsForEnv, getBuildLog, type Build } from '@/services/api'
import { Section } from '@/components/ui/section'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

interface EnrichedBuild extends Build {
  project_id: string
  project_name: string
  branch: string
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
          all.push({ ...b, project_id: p.id, project_name: p.name, branch: env.branch })
        }
      } catch {
        // skip
      }
    }
  }
  all.sort((a, b) => (a.started_at < b.started_at ? 1 : -1))
  return all
}

function statusVariant(status: string): 'success' | 'failed' | 'pending' | 'default' {
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

interface LogPanelProps {
  build: EnrichedBuild
  onClose: () => void
}

function isLegacyLogPath(path?: string): boolean {
  // Builds before the per-build log retention fix all share `latest.log`
  // (overwritten on every new build). The /api/v1/builds/{id}/log endpoint
  // 404s for them.
  return !!path && path.endsWith('/latest.log')
}

function LogPanel({ build, onClose }: LogPanelProps) {
  const legacy = isLegacyLogPath(build.log_path)
  const { data, isLoading, error } = useQuery({
    queryKey: ['build-log', build.id],
    queryFn: () => getBuildLog(build.id),
    enabled: !legacy,
  })
  return (
    <Section
      title={
        <span className="font-mono text-xs">
          {build.project_name} / {build.branch} · {build.id.slice(0, 8)} · {build.status}
        </span>
      }
      action={<Button variant="ghost" size="sm" onClick={onClose}>Close</Button>}
    >
      {legacy && (
        <div className="text-xs text-muted-foreground">
          Log not retained — this build predates per-build log retention.
        </div>
      )}
      {!legacy && isLoading && <div className="text-xs text-muted-foreground">loading log…</div>}
      {!legacy && error && (
        <div className="text-xs text-red-400">
          {(error as Error).message}
        </div>
      )}
      {!legacy && data && (
        <pre className="text-xs font-mono whitespace-pre-wrap bg-background border border-border p-3 rounded max-h-[60vh] overflow-y-auto">
          {data || '(empty)'}
        </pre>
      )}
    </Section>
  )
}

export default function Builds() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['builds', 'all'],
    queryFn: fetchAllBuilds,
    refetchInterval: 10000,
  })
  const [selected, setSelected] = useState<EnrichedBuild | null>(null)

  return (
    <div className="p-6 space-y-4 max-w-6xl">
      <header>
        <h1 className="text-xl font-semibold">Builds</h1>
        <p className="text-sm text-muted-foreground">Recent build history across all projects.</p>
      </header>

      <Section flush>
        {isLoading && <div className="text-sm text-muted-foreground p-4">loading…</div>}
        {error && <div className="text-sm text-red-400 p-4">{(error as Error).message}</div>}
        {data && data.length === 0 && (
          <div className="text-sm text-muted-foreground p-4">No builds yet.</div>
        )}
        {data && data.length > 0 && (
          <table className="w-full text-xs">
            <thead className="text-[11px] text-muted-foreground uppercase tracking-wider">
              <tr className="border-b border-border">
                <th className="text-left py-2.5 px-1 font-medium">Project</th>
                <th className="text-left py-2.5 px-1 font-medium">Branch</th>
                <th className="text-left py-2.5 px-1 font-medium">SHA</th>
                <th className="text-left py-2.5 px-1 font-medium">Status</th>
                <th className="text-left py-2.5 px-1 font-medium">Trigger</th>
                <th className="text-left py-2.5 px-1 font-medium tabular-nums">Started</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {data.slice(0, 50).map((b) => (
                <tr
                  key={`${b.env_id}-${b.id}`}
                  className="border-b border-border last:border-0 hover:bg-muted/40"
                >
                  <td className="py-2.5 px-1">
                    <Link to={`/projects/${b.project_id}`} className="font-medium hover:text-primary transition-colors">
                      {b.project_name}
                    </Link>
                  </td>
                  <td className="py-2.5 px-1 text-muted-foreground font-mono">{b.branch}</td>
                  <td className="py-2.5 px-1 font-mono text-muted-foreground">{b.sha?.slice(0, 7) || '—'}</td>
                  <td className="py-2.5 px-1">
                    <Badge variant={statusVariant(b.status)}>{b.status}</Badge>
                  </td>
                  <td className="py-2.5 px-1 text-muted-foreground">{b.triggered_by}</td>
                  <td className="py-2.5 px-1 text-muted-foreground tabular-nums">{relativeTime(b.started_at)}</td>
                  <td className="py-2.5 px-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-xs h-7"
                      onClick={() => setSelected(b)}
                    >
                      Logs
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Section>

      {selected && <LogPanel build={selected} onClose={() => setSelected(null)} />}
    </div>
  )
}
