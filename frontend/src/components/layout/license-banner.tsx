import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AlertTriangle } from 'lucide-react'
import { getSettings } from '@/services/api'

// LicenseBanner shows a persistent warning when the server reports an
// invalid license. Only renders when settings.license.valid === false; in
// the publisher's own homelab (LICENSE_ENFORCE off) the server reports a
// synthetic valid status so the banner never appears.
export function LicenseBanner() {
  const settings = useQuery({
    queryKey: ['settings'],
    queryFn: getSettings,
    refetchInterval: 60_000,
  })
  const lic = settings.data?.license
  if (!lic || lic.valid) return null

  return (
    <div className="border-b border-destructive/40 bg-destructive/10 px-4 py-2 flex items-center gap-2 text-[12px] text-destructive">
      <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
      <span className="font-medium uppercase tracking-wider text-[10px]">License invalid</span>
      <span className="text-destructive/80 truncate">— {lic.reason || 'unknown reason'}. Mutating endpoints are blocked.</span>
      <Link
        to="/settings"
        className="ml-auto underline hover:text-foreground transition-colors shrink-0"
      >
        Settings →
      </Link>
    </div>
  )
}
