import { Link, useLocation } from "react-router-dom"
import {
  Home,
  Rocket,
  Hammer,
  Database,
  Network,
  Settings,
  ChevronLeft,
  ChevronRight,
  Box,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from "@/components/ui/tooltip"
import { useUIStore } from "@/stores/ui-store"

interface NavItem {
  title: string
  href: string
  icon: React.ComponentType<{ className?: string }>
}

const navItems: NavItem[] = [
  { title: "Overview", href: "/", icon: Home },
  { title: "Projects", href: "/projects", icon: Rocket },
  { title: "Services", href: "/services", icon: Database },
  { title: "Topology", href: "/topology", icon: Network },
  { title: "Builds", href: "/builds", icon: Hammer },
  { title: "Settings", href: "/settings", icon: Settings },
]

export function Sidebar() {
  const location = useLocation()
  const { sidebarCollapsed, toggleSidebar } = useUIStore()

  return (
    <TooltipProvider delayDuration={0}>
      <aside
        className={cn(
          "hidden lg:flex flex-col h-screen bg-background border-r border-border transition-all duration-300",
          sidebarCollapsed ? "w-16" : "w-[220px]"
        )}
      >
        <div className="flex items-center justify-between h-[52px] px-4 border-b border-border">
          {!sidebarCollapsed && (
            <Link to="/" className="flex flex-col leading-tight min-w-0">
              <span className="flex items-center gap-2">
                <Box className="h-5 w-5 text-primary shrink-0" />
                <span className="font-semibold text-[15px] tracking-tight">Env Manager</span>
              </span>
              <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70 ml-7 mt-0.5">
                v2.4.0 · pro
              </span>
            </Link>
          )}
          <Button
            variant="ghost"
            size="icon"
            onClick={toggleSidebar}
            className={cn("h-7 w-7", sidebarCollapsed && "mx-auto")}
          >
            {sidebarCollapsed ? (
              <ChevronRight className="h-4 w-4" />
            ) : (
              <ChevronLeft className="h-4 w-4" />
            )}
          </Button>
        </div>

        <nav className="flex-1 py-3 overflow-y-auto">
          <ul className="space-y-0.5 px-2">
            {navItems.map((item) => {
              const isActive = location.pathname === item.href ||
                (item.href !== "/" && location.pathname.startsWith(item.href))

              const linkContent = (
                <Link
                  to={item.href}
                  className={cn(
                    "group relative flex items-center gap-3 pl-3 pr-3 py-2 rounded-md text-[13px] font-medium transition-colors",
                    // Active = subtle bg + 2px green left border (Stitch design)
                    isActive
                      ? "bg-secondary text-foreground before:absolute before:left-0 before:top-1.5 before:bottom-1.5 before:w-[2px] before:rounded-full before:bg-primary"
                      : "text-muted-foreground hover:bg-secondary/60 hover:text-foreground",
                    sidebarCollapsed && "justify-center px-2"
                  )}
                >
                  <item.icon className="h-4 w-4 shrink-0" />
                  {!sidebarCollapsed && <span>{item.title}</span>}
                </Link>
              )

              if (sidebarCollapsed) {
                return (
                  <li key={item.href}>
                    <Tooltip>
                      <TooltipTrigger asChild>{linkContent}</TooltipTrigger>
                      <TooltipContent side="right">{item.title}</TooltipContent>
                    </Tooltip>
                  </li>
                )
              }

              return <li key={item.href}>{linkContent}</li>
            })}
          </ul>
        </nav>

      </aside>
    </TooltipProvider>
  )
}
