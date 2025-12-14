import { Link, useLocation } from "react-router-dom"
import { Menu, Bell, RefreshCw, GitBranch } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTrigger, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Badge } from "@/components/ui/badge"
import { useUIStore } from "@/stores/ui-store"
import {
  LayoutDashboard,
  Box,
  HardDrive,
  Layers,
  Network,
  Settings,
} from "lucide-react"
import { cn } from "@/lib/utils"

const navItems = [
  { title: "Dashboard", href: "/", icon: LayoutDashboard },
  { title: "Containers", href: "/containers", icon: Box },
  { title: "Volumes", href: "/volumes", icon: HardDrive },
  { title: "Compose", href: "/compose", icon: Layers },
  { title: "Network", href: "/network", icon: Network },
  { title: "Git", href: "/git", icon: GitBranch },
  { title: "Settings", href: "/settings", icon: Settings },
]

export function Header() {
  const location = useLocation()
  const { sidebarMobileOpen, setSidebarMobileOpen } = useUIStore()

  return (
    <header className="sticky top-0 z-40 h-14 border-b border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
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
            <div className="flex items-center h-14 px-4 border-b border-border">
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
                            ? "bg-primary text-primary-foreground"
                            : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
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
          <Box className="h-6 w-6 text-primary" />
          <span className="font-semibold">Env Manager</span>
        </div>

        <div className="flex-1" />

        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" title="Refresh">
            <RefreshCw className="h-4 w-4" />
          </Button>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" className="relative">
                <Bell className="h-4 w-4" />
                <Badge
                  variant="destructive"
                  className="absolute -top-1 -right-1 h-4 w-4 p-0 flex items-center justify-center text-[10px]"
                >
                  3
                </Badge>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-80">
              <DropdownMenuLabel>Notifications</DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem>
                <div className="flex flex-col gap-1">
                  <span className="font-medium">Container stopped</span>
                  <span className="text-xs text-muted-foreground">
                    nginx-proxy exited with code 0
                  </span>
                </div>
              </DropdownMenuItem>
              <DropdownMenuItem>
                <div className="flex flex-col gap-1">
                  <span className="font-medium">Git sync completed</span>
                  <span className="text-xs text-muted-foreground">
                    Pulled 3 new commits from origin/main
                  </span>
                </div>
              </DropdownMenuItem>
              <DropdownMenuItem>
                <div className="flex flex-col gap-1">
                  <span className="font-medium">High memory usage</span>
                  <span className="text-xs text-muted-foreground">
                    postgres container using 85% memory
                  </span>
                </div>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          <Badge variant="outline" className="hidden sm:flex gap-1 text-xs">
            <GitBranch className="h-3 w-3" />
            main
          </Badge>
        </div>
      </div>
    </header>
  )
}
