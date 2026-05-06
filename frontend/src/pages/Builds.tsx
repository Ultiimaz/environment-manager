import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { listProjects, getProject, listBuildsForEnv, type Build } from '@/services/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

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
        // Skip envs whose build list errors; continue aggregating.
      }
    }
  }
  // Most-recent first; already sorted by API but resort across envs.
  all.sort((a, b) => (a.started_at < b.started_at ? 1 : -1))
  return all
}

function statusVariant(status: string): 'default' | 'destructive' | 'secondary' {
  switch (status) {
    case 'success':
      return 'default'
    case 'failed':
    case 'cancelled':
      return 'destructive'
    default:
      return 'secondary'
  }
}

export default function Builds() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['builds', 'all'],
    queryFn: fetchAllBuilds,
    refetchInterval: 10000,
  })

  return (
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">Builds</h1>

      <Card>
        <CardHeader>
          <CardTitle>Recent builds</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading && <div className="text-sm text-muted-foreground">loading…</div>}
          {error && <div className="text-sm text-destructive">{(error as Error).message}</div>}
          {data && data.length === 0 && (
            <div className="text-sm text-muted-foreground">No builds yet.</div>
          )}
          {data && data.length > 0 && (
            <table className="w-full text-sm">
              <thead className="text-xs text-muted-foreground">
                <tr className="border-b">
                  <th className="text-left py-2">Project</th>
                  <th className="text-left py-2">Branch</th>
                  <th className="text-left py-2">SHA</th>
                  <th className="text-left py-2">Status</th>
                  <th className="text-left py-2">Triggered</th>
                  <th className="text-left py-2">Started</th>
                </tr>
              </thead>
              <tbody>
                {data.slice(0, 50).map((b) => (
                  <tr key={`${b.env_id}-${b.id}`} className="border-b last:border-0">
                    <td className="py-2">
                      <Link to={`/projects/${b.project_id}`} className="hover:underline font-medium">
                        {b.project_name}
                      </Link>
                    </td>
                    <td className="py-2 text-muted-foreground">{b.branch}</td>
                    <td className="py-2 font-mono text-xs">{b.sha?.slice(0, 7) || '—'}</td>
                    <td className="py-2">
                      <Badge variant={statusVariant(b.status)}>{b.status}</Badge>
                    </td>
                    <td className="py-2 text-xs text-muted-foreground">{b.triggered_by}</td>
                    <td className="py-2 text-xs text-muted-foreground">{b.started_at}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
