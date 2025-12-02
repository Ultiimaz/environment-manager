import { useState } from 'react'
import { Outlet, NavLink } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  LayoutDashboard,
  Box,
  HardDrive,
  Layers,
  Network,
  Settings,
  GitBranch,
  RefreshCw,
  Menu,
  X
} from 'lucide-react'
import { getNetworkStatus, getGitStatus, syncGit } from '../services/api'

const navItems = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard' },
  { to: '/containers', icon: Box, label: 'Containers' },
  { to: '/volumes', icon: HardDrive, label: 'Volumes' },
  { to: '/compose', icon: Layers, label: 'Compose' },
  { to: '/network', icon: Network, label: 'Network' },
  { to: '/settings', icon: Settings, label: 'Settings' },
]

export default function Layout() {
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const { data: networkStatus } = useQuery({
    queryKey: ['networkStatus'],
    queryFn: getNetworkStatus,
    refetchInterval: 30000,
  })

  const { data: gitStatus, refetch: refetchGitStatus } = useQuery({
    queryKey: ['gitStatus'],
    queryFn: getGitStatus,
    refetchInterval: 60000,
  })

  const handleSync = async () => {
    try {
      await syncGit()
      refetchGitStatus()
    } catch (error) {
      console.error('Sync failed:', error)
    }
  }

  return (
    <div className="flex h-screen bg-gray-900">
      {/* Mobile menu button */}
      <button
        onClick={() => setSidebarOpen(!sidebarOpen)}
        className="lg:hidden fixed top-4 left-4 z-50 p-2 bg-gray-800 rounded-lg text-white"
      >
        {sidebarOpen ? <X size={24} /> : <Menu size={24} />}
      </button>

      {/* Overlay for mobile */}
      {sidebarOpen && (
        <div
          className="lg:hidden fixed inset-0 bg-black/50 z-30"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside className={`
        fixed lg:static inset-y-0 left-0 z-40
        w-64 bg-gray-800 border-r border-gray-700 flex flex-col
        transform transition-transform duration-300 ease-in-out
        ${sidebarOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'}
      `}>
        <div className="p-4 border-b border-gray-700">
          <h1 className="text-xl font-bold text-white">Environment Manager</h1>
          <p className="text-sm text-gray-400 mt-1">
            {networkStatus?.base_domain || 'localhost'}
          </p>
        </div>

        <nav className="flex-1 p-4 overflow-y-auto">
          <ul className="space-y-2">
            {navItems.map(({ to, icon: Icon, label }) => (
              <li key={to}>
                <NavLink
                  to={to}
                  onClick={() => setSidebarOpen(false)}
                  className={({ isActive }) =>
                    `flex items-center gap-3 px-3 py-2 rounded-lg transition-colors ${
                      isActive
                        ? 'bg-blue-600 text-white'
                        : 'text-gray-300 hover:bg-gray-700 hover:text-white'
                    }`
                  }
                >
                  <Icon size={20} />
                  {label}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>

        {/* Git Status */}
        <div className="p-4 border-t border-gray-700">
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-2 text-sm text-gray-400">
              <GitBranch size={16} />
              <span>Git Status</span>
            </div>
            <button
              onClick={handleSync}
              className="p-1 hover:bg-gray-700 rounded"
              title="Sync from Git"
            >
              <RefreshCw size={16} className="text-gray-400" />
            </button>
          </div>
          <div className={`text-sm ${gitStatus?.clean ? 'text-green-400' : 'text-yellow-400'}`}>
            {gitStatus?.clean ? 'Clean' : `${gitStatus?.changed_files.length || 0} changes`}
          </div>
        </div>

        {/* Infrastructure Status */}
        <div className="p-4 border-t border-gray-700">
          <div className="text-sm text-gray-400 mb-2">Infrastructure</div>
          <div className="space-y-1">
            <StatusItem
              label="Traefik"
              status={networkStatus?.traefik_status || 'unknown'}
            />
            <StatusItem
              label="CoreDNS"
              status={networkStatus?.coredns_status || 'unknown'}
            />
          </div>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-auto lg:ml-0 pt-16 lg:pt-0">
        <div className="p-4 sm:p-6 lg:p-8">
          <Outlet />
        </div>
      </main>
    </div>
  )
}

function StatusItem({ label, status }: { label: string; status: string }) {
  const getStatusColor = (status: string) => {
    switch (status) {
      case 'running':
        return 'bg-green-500'
      case 'exited':
      case 'stopped':
        return 'bg-red-500'
      case 'not_found':
        return 'bg-gray-500'
      default:
        return 'bg-yellow-500'
    }
  }

  return (
    <div className="flex items-center justify-between text-sm">
      <span className="text-gray-300">{label}</span>
      <div className="flex items-center gap-2">
        <span className={`w-2 h-2 rounded-full ${getStatusColor(status)}`} />
        <span className="text-gray-400 capitalize">{status}</span>
      </div>
    </div>
  )
}
