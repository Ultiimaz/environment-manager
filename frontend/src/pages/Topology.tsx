import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { getTopology, type TopologyNode, type TopologyEdge } from '@/services/api'
import { cn } from '@/lib/utils'

// Stitch Topology design (.design/stitch-v1/01-topology.html)
// Single green accent, dense info, monochrome edges, nodes table below.

interface PositionedNode extends TopologyNode {
  x: number
  y: number
}

const NODE_WIDTH = 200
const NODE_HEIGHT = 60
const HORIZONTAL_GAP = 40
const VERTICAL_GAP = 140

const STATUS_TOKENS = {
  running: { dot: 'bg-primary', text: 'text-primary', bg: 'bg-primary/10', border: 'border-primary/30' },
  building: { dot: 'bg-warning', text: 'text-warning', bg: 'bg-warning/10', border: 'border-warning/30' },
  pending: { dot: 'bg-warning', text: 'text-warning', bg: 'bg-warning/10', border: 'border-warning/30' },
  stopped: { dot: 'bg-destructive', text: 'text-destructive', bg: 'bg-destructive/10', border: 'border-destructive/30' },
  failed: { dot: 'bg-destructive', text: 'text-destructive', bg: 'bg-destructive/10', border: 'border-destructive/30' },
  idle: { dot: 'bg-muted-foreground', text: 'text-muted-foreground', bg: 'bg-secondary', border: 'border-border' },
} as const

type StatusKey = keyof typeof STATUS_TOKENS

function statusKey(status?: string): StatusKey {
  if (!status) return 'idle'
  if (status in STATUS_TOKENS) return status as StatusKey
  return 'idle'
}

function statusStrokeFor(status?: string): string {
  switch (statusKey(status)) {
    case 'running':
      return 'hsl(var(--primary))'
    case 'building':
    case 'pending':
      return 'hsl(var(--warning))'
    case 'stopped':
    case 'failed':
      return 'hsl(var(--destructive))'
    default:
      return 'hsl(var(--muted-foreground))'
  }
}

function layout(nodes: TopologyNode[]): { positioned: PositionedNode[]; width: number; height: number } {
  const services = nodes.filter((n) => n.type === 'service')
  const envs = nodes.filter((n) => n.type === 'env')
  const widthForRow = (count: number) =>
    Math.max(NODE_WIDTH, count * NODE_WIDTH + Math.max(0, count - 1) * HORIZONTAL_GAP)
  const totalWidth = Math.max(widthForRow(services.length), widthForRow(envs.length), 600)
  const positionRow = (row: TopologyNode[], y: number): PositionedNode[] => {
    const w = widthForRow(row.length)
    const startX = (totalWidth - w) / 2
    return row.map((n, i) => ({
      ...n,
      x: startX + i * (NODE_WIDTH + HORIZONTAL_GAP),
      y,
    }))
  }
  const positioned: PositionedNode[] = [
    ...positionRow(services, 32),
    ...positionRow(envs, 32 + NODE_HEIGHT + VERTICAL_GAP),
  ]
  const totalHeight = 32 + 2 * NODE_HEIGHT + VERTICAL_GAP + 32
  return { positioned, width: totalWidth, height: totalHeight }
}

function NodeBox({ node, onClick }: { node: PositionedNode; onClick: () => void }) {
  const isService = node.type === 'service'
  const stroke = statusStrokeFor(node.status)
  return (
    <g
      className="cursor-pointer"
      onClick={onClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') onClick()
      }}
    >
      <rect
        x={node.x}
        y={node.y}
        width={NODE_WIDTH}
        height={NODE_HEIGHT}
        rx={isService ? 30 : 8}
        ry={isService ? 30 : 8}
        fill="hsl(var(--card))"
        stroke="hsl(var(--border))"
        strokeWidth={1}
        className="transition-colors hover:stroke-[hsl(var(--primary))]"
      />
      <circle cx={node.x + 14} cy={node.y + NODE_HEIGHT / 2} r={3} fill={stroke} />
      <text
        x={node.x + 26}
        y={node.y + 24}
        fill="hsl(var(--foreground))"
        fontSize={13}
        fontWeight={500}
        fontFamily="'Geist Mono', ui-monospace, monospace"
        className="select-none"
      >
        {truncate(node.label, 22)}
      </text>
      <text
        x={node.x + 26}
        y={node.y + 42}
        fill="hsl(var(--muted-foreground))"
        fontSize={10}
        fontFamily="'Geist Mono', ui-monospace, monospace"
        className="select-none"
      >
        {isService ? node.image : `${node.kind || ''} · ${node.status || ''}`}
      </text>
      <text
        x={node.x + NODE_WIDTH - 12}
        y={node.y + 18}
        fill="hsl(var(--muted-foreground))"
        fontSize={9}
        textAnchor="end"
        className="uppercase tracking-widest select-none"
      >
        {node.type}
      </text>
    </g>
  )
}

function truncate(s: string, max: number): string {
  return s.length <= max ? s : s.slice(0, max - 1) + '…'
}

function EdgePath({
  from,
  to,
  kind,
}: {
  from: PositionedNode
  to: PositionedNode
  kind: string
}) {
  const x1 = from.x + NODE_WIDTH / 2
  const y1 = from.y
  const x2 = to.x + NODE_WIDTH / 2
  const y2 = to.y + NODE_HEIGHT
  const midY = (y1 + y2) / 2
  const path = `M ${x1} ${y1} C ${x1} ${midY}, ${x2} ${midY}, ${x2} ${y2}`
  const opacity = 0.4
  const color =
    kind === 'postgres'
      ? 'hsl(var(--primary))'
      : kind === 'redis'
      ? 'hsl(var(--destructive))'
      : 'hsl(var(--muted-foreground))'
  return <path d={path} fill="none" stroke={color} strokeWidth={1} strokeOpacity={opacity} />
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

function StatusPill({ status }: { status?: string }) {
  const key = statusKey(status)
  const tok = STATUS_TOKENS[key]
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
      {status || 'idle'}
    </span>
  )
}

export default function Topology() {
  const navigate = useNavigate()
  const { data, isLoading, error } = useQuery({
    queryKey: ['topology'],
    queryFn: getTopology,
    refetchInterval: 15000,
  })

  const counts = (() => {
    if (!data) return { running: 0, building: 0, failed: 0, envs: 0 }
    let running = 0,
      building = 0,
      failed = 0,
      envs = 0
    for (const n of data.nodes) {
      if (n.type === 'env') envs++
      const k = statusKey(n.status)
      if (k === 'running') running++
      if (k === 'building' || k === 'pending') building++
      if (k === 'stopped' || k === 'failed') failed++
    }
    return { running, building, failed, envs }
  })()

  return (
    <div className="px-6 py-6 space-y-6 max-w-[1400px] mx-auto">
      {/* Header */}
      <header>
        <h1 className="text-[28px] font-semibold tracking-tight leading-none">Topology</h1>
        <p className="mt-1.5 text-sm text-muted-foreground">
          Live graph of singleton services and the environments that consume them. Click a node to drill in.
        </p>
      </header>

      {/* Metric tiles */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <MetricTile label="Running services" value={counts.running} />
        <MetricTile label="Building" value={counts.building} />
        <MetricTile label="Failed" value={counts.failed} />
        <MetricTile label="Total environments" value={counts.envs} />
      </div>

      {isLoading && (
        <div className="rounded-lg border border-border bg-card p-6 text-[12px] text-muted-foreground">
          loading topology…
        </div>
      )}

      {error && (
        <div className="rounded-lg border border-destructive/40 bg-destructive/10 p-4 text-[12px] text-destructive">
          {(error as Error).message}
        </div>
      )}

      {data &&
        (() => {
          const { positioned, width, height } = layout(data.nodes)
          const byId = new Map(positioned.map((n) => [n.id, n]))
          return (
            <>
              {/* Graph canvas */}
              <div className="rounded-lg border border-border bg-card overflow-x-auto">
                <svg
                  width={width + 64}
                  height={height}
                  viewBox={`0 0 ${width + 64} ${height}`}
                  className="block mx-auto"
                  role="img"
                  aria-label="Topology graph"
                >
                  <g transform="translate(32, 0)">
                    {data.edges.map((e: TopologyEdge, i: number) => {
                      const from = byId.get(e.from)
                      const to = byId.get(e.to)
                      if (!from || !to) return null
                      return <EdgePath key={i} from={from} to={to} kind={e.kind} />
                    })}
                    {positioned.map((n) => (
                      <NodeBox key={n.id} node={n} onClick={() => navigate(n.href)} />
                    ))}
                  </g>
                </svg>
              </div>

              {/* Legend strip */}
              <div className="rounded-lg border border-border bg-card px-4 py-3">
                <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-[11px] text-muted-foreground">
                  <span className="text-[10px] uppercase tracking-wider font-semibold text-foreground">
                    Legend
                  </span>
                  <span className="inline-flex items-center gap-1.5">
                    <span className="h-1.5 w-1.5 rounded-full bg-primary" /> running
                  </span>
                  <span className="inline-flex items-center gap-1.5">
                    <span className="h-1.5 w-1.5 rounded-full bg-warning" /> building
                  </span>
                  <span className="inline-flex items-center gap-1.5">
                    <span className="h-1.5 w-1.5 rounded-full bg-destructive" /> failed
                  </span>
                  <span className="inline-flex items-center gap-1.5">
                    <span className="h-1.5 w-1.5 rounded-full bg-muted-foreground" /> idle
                  </span>
                  <span className="ml-auto inline-flex items-center gap-1.5">
                    <span className="inline-block w-5 h-px bg-primary" /> postgres edge
                  </span>
                  <span className="inline-flex items-center gap-1.5">
                    <span className="inline-block w-5 h-px bg-destructive" /> redis edge
                  </span>
                </div>
              </div>

              {/* Nodes table */}
              <div className="rounded-lg border border-border bg-card overflow-hidden">
                <div className="flex items-center justify-between px-4 py-3 border-b border-border">
                  <h2 className="text-[13px] font-semibold uppercase tracking-wider">
                    Topology entities ({data.nodes.length})
                  </h2>
                </div>
                <table className="w-full text-[12px]">
                  <thead>
                    <tr className="text-left text-[10px] uppercase tracking-wider text-muted-foreground/80 border-b border-border">
                      <th className="px-4 py-2 font-medium">Type</th>
                      <th className="px-4 py-2 font-medium">Name</th>
                      <th className="px-4 py-2 font-medium">State</th>
                      <th className="px-4 py-2 font-medium">Detail</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {data.nodes.map((n) => (
                      <tr
                        key={n.id}
                        onClick={() => navigate(n.href)}
                        className="cursor-pointer hover:bg-secondary/40 transition-colors"
                      >
                        <td className="px-4 py-2.5">
                          <span className="inline-flex items-center rounded-full border border-border bg-secondary px-2 py-0.5 text-[10px] uppercase tracking-wider text-muted-foreground">
                            {n.type}
                          </span>
                        </td>
                        <td className="px-4 py-2.5 font-mono">{n.label}</td>
                        <td className="px-4 py-2.5">
                          <StatusPill status={n.status} />
                        </td>
                        <td className="px-4 py-2.5 text-muted-foreground font-mono text-[11px]">
                          {n.type === 'service' ? n.image : n.kind || '—'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )
        })()}
    </div>
  )
}
