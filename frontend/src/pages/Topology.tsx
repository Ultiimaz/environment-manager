import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { getTopology, type TopologyNode, type TopologyEdge } from '@/services/api'
import { Section } from '@/components/ui/section'
import { Badge } from '@/components/ui/badge'

interface PositionedNode extends TopologyNode {
  x: number
  y: number
}

const NODE_WIDTH = 200
const NODE_HEIGHT = 64
const HORIZONTAL_GAP = 32
const VERTICAL_GAP = 120

function statusVariant(status?: string): 'success' | 'failed' | 'pending' | 'default' {
  switch (status) {
    case 'running': return 'success'
    case 'stopped':
    case 'failed': return 'failed'
    case 'building':
    case 'pending': return 'pending'
    default: return 'default'
  }
}

function statusFill(status?: string): string {
  switch (status) {
    case 'running': return 'rgb(52 211 153 / 0.3)' // emerald-400/30
    case 'stopped':
    case 'failed': return 'rgb(248 113 113 / 0.3)' // red-400/30
    case 'building':
    case 'pending': return 'rgb(251 191 36 / 0.3)' // amber-400/30
    default: return 'rgb(115 115 115 / 0.2)' // neutral-500/20
  }
}

function layout(nodes: TopologyNode[]): { positioned: PositionedNode[]; width: number; height: number } {
  const services = nodes.filter((n) => n.type === 'service')
  const envs = nodes.filter((n) => n.type === 'env')

  const widthForRow = (count: number) =>
    Math.max(NODE_WIDTH, count * NODE_WIDTH + Math.max(0, count - 1) * HORIZONTAL_GAP)

  const totalWidth = Math.max(widthForRow(services.length), widthForRow(envs.length), 480)

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
    ...positionRow(services, 24),
    ...positionRow(envs, 24 + NODE_HEIGHT + VERTICAL_GAP),
  ]
  const totalHeight = 24 + 2 * NODE_HEIGHT + VERTICAL_GAP + 24
  return { positioned, width: totalWidth, height: totalHeight }
}

interface NodeBoxProps {
  node: PositionedNode
  onClick: () => void
}

function NodeBox({ node, onClick }: NodeBoxProps) {
  const isService = node.type === 'service'
  return (
    <g
      className="cursor-pointer"
      onClick={onClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') onClick() }}
    >
      <rect
        x={node.x}
        y={node.y}
        width={NODE_WIDTH}
        height={NODE_HEIGHT}
        rx={8}
        ry={8}
        fill="hsl(var(--card))"
        stroke="hsl(var(--border))"
        strokeWidth={1}
        className="transition-colors hover:stroke-[hsl(var(--primary))]"
      />
      <rect
        x={node.x}
        y={node.y}
        width={6}
        height={NODE_HEIGHT}
        rx={3}
        ry={3}
        fill={statusFill(node.status)}
      />
      <text
        x={node.x + 16}
        y={node.y + 24}
        fill="hsl(var(--foreground))"
        fontSize={13}
        fontWeight={500}
        className="select-none"
      >
        {truncate(node.label, 22)}
      </text>
      <text
        x={node.x + 16}
        y={node.y + 42}
        fill="hsl(var(--muted-foreground))"
        fontSize={11}
        fontFamily="ui-monospace, SFMono-Regular, Menlo, monospace"
        className="select-none"
      >
        {isService ? node.image : `${node.kind || ''} · ${node.status || ''}`}
      </text>
      <text
        x={node.x + NODE_WIDTH - 12}
        y={node.y + 22}
        fill="hsl(var(--muted-foreground))"
        fontSize={10}
        textAnchor="end"
        className="uppercase tracking-wider select-none"
      >
        {node.type}
      </text>
    </g>
  )
}

function truncate(s: string, max: number): string {
  if (s.length <= max) return s
  return s.slice(0, max - 1) + '…'
}

interface EdgePathProps {
  from: PositionedNode
  to: PositionedNode
  kind: string
}

function EdgePath({ from, to, kind }: EdgePathProps) {
  // env nodes sit below services, edges go upward
  const x1 = from.x + NODE_WIDTH / 2
  const y1 = from.y
  const x2 = to.x + NODE_WIDTH / 2
  const y2 = to.y + NODE_HEIGHT
  const midY = (y1 + y2) / 2
  const path = `M ${x1} ${y1} C ${x1} ${midY}, ${x2} ${midY}, ${x2} ${y2}`
  const color = kind === 'postgres' ? 'rgb(56 189 248)' : kind === 'redis' ? 'rgb(248 113 113)' : 'hsl(var(--muted-foreground))'
  return (
    <g>
      <path d={path} fill="none" stroke={color} strokeWidth={1.25} strokeOpacity={0.5} />
    </g>
  )
}

export default function Topology() {
  const navigate = useNavigate()
  const { data, isLoading, error } = useQuery({
    queryKey: ['topology'],
    queryFn: getTopology,
    refetchInterval: 15000,
  })

  return (
    <div className="p-6 space-y-4 max-w-6xl">
      <header>
        <h1 className="text-xl font-semibold">Topology</h1>
        <p className="text-sm text-muted-foreground">
          Live graph of singleton services and the environments that consume them. Click a node to drill in.
        </p>
      </header>

      {isLoading && (
        <Section>
          <div className="text-xs text-muted-foreground">loading topology…</div>
        </Section>
      )}

      {error && (
        <Section className="border-red-900/60">
          <div className="text-xs text-red-400">{(error as Error).message}</div>
        </Section>
      )}

      {data && (() => {
        const { positioned, width, height } = layout(data.nodes)
        const byId = new Map(positioned.map((n) => [n.id, n]))
        return (
          <>
            <Section flush className="overflow-x-auto">
              <svg
                width={width + 32}
                height={height}
                viewBox={`0 0 ${width + 32} ${height}`}
                className="block mx-auto"
                role="img"
                aria-label="Topology graph"
              >
                <g transform="translate(16, 0)">
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
            </Section>

            <Section title="Legend">
              <div className="flex flex-wrap gap-3 text-xs">
                <span className="inline-flex items-center gap-2">
                  <span className="inline-block w-3 h-3 rounded-sm" style={{ background: 'rgb(52 211 153 / 0.5)' }} />
                  running
                </span>
                <span className="inline-flex items-center gap-2">
                  <span className="inline-block w-3 h-3 rounded-sm" style={{ background: 'rgb(248 113 113 / 0.5)' }} />
                  stopped / failed
                </span>
                <span className="inline-flex items-center gap-2">
                  <span className="inline-block w-3 h-3 rounded-sm" style={{ background: 'rgb(251 191 36 / 0.5)' }} />
                  pending / building
                </span>
                <span className="inline-flex items-center gap-2">
                  <span className="inline-block w-4 h-px" style={{ background: 'rgb(56 189 248)' }} />
                  postgres edge
                </span>
                <span className="inline-flex items-center gap-2">
                  <span className="inline-block w-4 h-px" style={{ background: 'rgb(248 113 113)' }} />
                  redis edge
                </span>
              </div>
            </Section>

            <Section
              title={`Nodes (${data.nodes.length})`}
              flush
            >
              <ul className="divide-y divide-border -mx-4 -mb-1">
                {data.nodes.map((n) => (
                  <li
                    key={n.id}
                    onClick={() => navigate(n.href)}
                    className="flex items-center justify-between gap-4 px-4 py-2 hover:bg-muted/40 cursor-pointer transition-colors"
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <Badge variant="default">{n.type}</Badge>
                      <span className="text-sm truncate">{n.label}</span>
                    </div>
                    <Badge variant={statusVariant(n.status)}>{n.status || '—'}</Badge>
                  </li>
                ))}
              </ul>
            </Section>
          </>
        )
      })()}
    </div>
  )
}
