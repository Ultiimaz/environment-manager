import { Link, useLocation } from "react-router-dom"
import { Menu, RefreshCw, Rocket as Box, Search, ChevronRight } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTrigger, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { useUIStore } from "@/stores/ui-store"
import { Home, Rocket, Hammer, Database, Network, Settings as SettingsIcon } from "lucide-react"
import { cn } from "@/lib/utils"

const navItems = [
  { title: "Overview", href: "/", icon: Home },
  { title: "Projects", href: "/projects", icon: Rocket },
  { title: "Builds", href: "/builds", icon: Hammer },
  { title: "Services", href: "/services", icon: Database },
  { title: "Topology", href: "/topology", icon: Network },
  { title: "Settings", href: "/settings", icon: SettingsIcon },
]

// Build a breadcrumb trail from the current pathname.
// Example: /projects/stripe-payments/envs/main → ["Home Lab", "Projects", "stripe-payments", "main"]
function useBreadcrumb(): string[] {
  const location = useLocation()
  const segs = location.pathname.split("/").filter(Boolean)
  const crumbs = ["Home Lab"]
  if (segs.length === 0) {
    crumbs.push("Overview")
    return crumbs
  }
  // Capitalize first segment as section title
  const sectionMap: Record<string, string> = {
    projects: "Projects",
    builds: "Builds",
    services: "Services",
    topology: "Topology",
    settings: "Network",
  }
  crumbs.push(sectionMap[segs[0]] ?? segs[0])
  // Skip the literal "envs" route segment, keep the values
  for (let i = 1; i < segs.length; i++) {
    if (segs[i] === "envs") continue
    crumbs.push(segs[i])
  }
  return crumbs
}

export function Header() {
  const location = useLocation()
  const { sidebarMobileOpen, setSidebarMobileOpen } = useUIStore()
  const crumbs = useBreadcrumb()

  return (
    <header className="sticky top-0 z-40 h-[52px] border-b border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="flex h-full items-center gap-4 px-4">
        <Sheet open={sidebarMobileOpen} onOpenChange={setSidebarMobileOpen}>
          <SheetTrigger asChild>
            <Button variant="ghost" size="icon" className="lg:hidden">
              <Menu className="h-5 w-5" />
              <span className="sr-only">Toggle menu</span>
            </Button>
          </SheetTrigger>
          <SheetContent side="left" className="w-64 p-0">
            <SheetTitle className="sr-only">Navigation Menu</SheetTitle>
            <SheetDescription className="sr-only">
              Main navigation for the application
            </SheetDescription>
            <div className="flex items-center h-[52px] px-4 border-b border-border">
              <Link
                to="/"
                className="flex items-center gap-2"
                onClick={() => setSidebarMobileOpen(false)}
              >
                <Box className="h-6 w-6 text-primary" />
                <span className="font-semibold text-lg">Env Manager</span>
              </Link>
            </div>
            <nav className="py-4">
              <ul className="space-y-1 px-2">
                {navItems.map((item) => {
                  const isActive =
                    location.pathname === item.href ||
                    (item.href !== "/" && location.pathname.startsWith(item.href))

                  return (
                    <li key={item.href}>
                      <Link
                        to={item.href}
                        onClick={() => setSidebarMobileOpen(false)}
                        className={cn(
                          "flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-colors",
                          isActive
                            ? "bg-secondary text-foreground"
                            : "text-muted-foreground hover:bg-secondary/60 hover:text-foreground"
                        )}
                      >
                        <item.icon className="h-5 w-5 shrink-0" />
                        <span>{item.title}</span>
                      </Link>
                    </li>
                  )
                })}
              </ul>
            </nav>
          </SheetContent>
        </Sheet>

        <div className="lg:hidden flex items-center gap-2">
          <Box className="h-5 w-5 text-primary" />
          <span className="font-semibold">Env Manager</span>
        </div>

        {/* Breadcrumb trail (desktop) */}
        <nav className="hidden lg:flex items-center gap-1.5 text-[13px] text-muted-foreground min-w-0">
          {crumbs.map((c, i) => (
            <span key={i} className="flex items-center gap-1.5 min-w-0">
              {i > 0 && <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0" />}
              <span
                className={cn(
                  "truncate",
                  i === crumbs.length - 1 ? "text-foreground font-medium" : ""
                )}
              >
                {c}
              </span>
            </span>
          ))}
        </nav>

        {/* Live status pill */}
        <span className="hidden md:inline-flex items-center gap-1.5 rounded-full bg-primary/10 border border-primary/20 px-2.5 py-0.5 text-[11px] font-medium uppercase tracking-wider text-primary">
          <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse" />
          12 services running
        </span>

        <div className="flex-1" />

        {/* Search hint (desktop) */}
        <button
          type="button"
          className="hidden md:inline-flex items-center gap-2 rounded-md border border-border bg-card px-2.5 py-1 text-[12px] text-muted-foreground hover:bg-secondary transition-colors"
          title="Search (⌘K)"
        >
          <Search className="h-3.5 w-3.5" />
          <span>Search</span>
          <kbd className="ml-2 rounded border border-border bg-secondary px-1.5 py-0.5 text-[10px] font-mono">⌘K</kbd>
        </button>

        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            title="Refresh"
            className="h-8 w-8"
            onClick={() => window.location.reload()}
          >
            <RefreshCw className="h-4 w-4" />
          </Button>
          {/* Avatar placeholder */}
          <div className="h-7 w-7 rounded-full bg-secondary border border-border flex items-center justify-center text-[11px] font-semibold uppercase text-muted-foreground">
            U
          </div>
        </div>
      </div>
    </header>
  )
}
