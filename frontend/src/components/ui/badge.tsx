import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"

/**
 * Badge — v2 design refresh. Subtle tinted pill (bg-{color}-950/40 +
 * border-{color}-900/60 + text-{color}-400) instead of solid-fill chips.
 *
 * Variant naming: success / failed / pending / default. Old shadcn variant
 * names map onto these for backward compat: default→success,
 * destructive→failed, secondary→default.
 */
const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium",
  {
    variants: {
      variant: {
        success:
          "bg-emerald-950/40 border-emerald-900/60 text-emerald-400",
        failed:
          "bg-red-950/40 border-red-900/60 text-red-400",
        pending:
          "bg-amber-950/40 border-amber-900/60 text-amber-400",
        default:
          "bg-muted border-border text-muted-foreground",
        // Legacy shadcn names — preserved for backward compat. Pages that
        // still pass these get the closest semantic match.
        destructive:
          "bg-red-950/40 border-red-900/60 text-red-400",
        secondary:
          "bg-muted border-border text-muted-foreground",
        outline:
          "bg-transparent border-border text-foreground",
        warning:
          "bg-amber-950/40 border-amber-900/60 text-amber-400",
        info:
          "bg-blue-950/40 border-blue-900/60 text-blue-400",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  )
}

export { Badge, badgeVariants }
