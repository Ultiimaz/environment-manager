import * as React from 'react'
import { cn } from '@/lib/utils'

interface SectionProps extends Omit<React.HTMLAttributes<HTMLElement>, 'title'> {
  /** Optional title rendered as a small h3 above the children. */
  title?: React.ReactNode
  /** Optional right-aligned action slot (e.g. a button) shown beside the title. */
  action?: React.ReactNode
  /** Disable internal vertical rhythm between children. Use for tight tables. */
  flush?: boolean
}

/**
 * Section — the v2 design's primary content container. Replaces the
 * shadcn Card/CardHeader/CardContent split with a single border + p-4 +
 * optional title-with-action header. Title is plain text, not a separate
 * subcomponent, so consumers can read the JSX as one block.
 */
export function Section({ title, action, flush, className, children, ...rest }: SectionProps) {
  return (
    <section
      className={cn(
        'rounded-lg border border-border bg-card p-4',
        !flush && 'space-y-3',
        className
      )}
      {...rest}
    >
      {(title || action) && (
        <div className="flex items-center justify-between">
          {title && <h3 className="text-sm font-medium text-foreground">{title}</h3>}
          {action && <div className="flex items-center gap-2">{action}</div>}
        </div>
      )}
      {children}
    </section>
  )
}
