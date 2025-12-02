import { useQuery } from '@tanstack/react-query'
import { GitBranch, RefreshCw, Clock } from 'lucide-react'
import { getGitStatus, getGitHistory, syncGit } from '../services/api'
import { useState } from 'react'

export default function Settings() {
  const [isSyncing, setIsSyncing] = useState(false)

  const { data: gitStatus, refetch: refetchStatus } = useQuery({
    queryKey: ['gitStatus'],
    queryFn: getGitStatus,
  })

  const { data: history = [], refetch: refetchHistory } = useQuery({
    queryKey: ['gitHistory'],
    queryFn: getGitHistory,
  })

  const handleSync = async () => {
    setIsSyncing(true)
    try {
      await syncGit()
      refetchStatus()
      refetchHistory()
    } catch (error) {
      console.error('Sync failed:', error)
      alert('Sync failed: ' + (error as Error).message)
    } finally {
      setIsSyncing(false)
    }
  }

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr)
    return date.toLocaleString()
  }

  return (
    <div className="p-6">
      <h1 className="text-2xl font-bold text-white mb-6">Settings</h1>

      {/* Git Status */}
      <div className="bg-gray-800 rounded-lg p-6 mb-6">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-3">
            <div className="p-2 bg-gray-700 rounded-lg text-gray-400">
              <GitBranch size={24} />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-white">Git Status</h2>
              <p className={`text-sm ${gitStatus?.clean ? 'text-green-400' : 'text-yellow-400'}`}>
                {gitStatus?.clean ? 'Working tree clean' : `${gitStatus?.changed_files.length || 0} uncommitted changes`}
              </p>
            </div>
          </div>
          <button
            onClick={handleSync}
            disabled={isSyncing}
            className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
          >
            <RefreshCw size={18} className={isSyncing ? 'animate-spin' : ''} />
            {isSyncing ? 'Syncing...' : 'Sync from Remote'}
          </button>
        </div>

        {gitStatus?.changed_files && gitStatus.changed_files.length > 0 && (
          <div className="mt-4 p-3 bg-gray-700 rounded-lg">
            <h3 className="text-sm text-gray-400 mb-2">Changed files:</h3>
            <ul className="space-y-1">
              {gitStatus.changed_files.map(file => (
                <li key={file} className="text-sm text-gray-300 font-mono">
                  {file}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>

      {/* Commit History */}
      <div className="bg-gray-800 rounded-lg p-6">
        <div className="flex items-center gap-3 mb-4">
          <div className="p-2 bg-gray-700 rounded-lg text-gray-400">
            <Clock size={24} />
          </div>
          <h2 className="text-lg font-semibold text-white">Recent Commits</h2>
        </div>

        <div className="space-y-3">
          {history.map(commit => (
            <div key={commit.hash} className="flex items-start gap-3 p-3 bg-gray-700 rounded-lg">
              <span className="text-blue-400 font-mono text-sm">{commit.hash}</span>
              <div className="flex-1 min-w-0">
                <p className="text-gray-300 text-sm truncate">{commit.message}</p>
                <p className="text-gray-500 text-xs mt-1">
                  {commit.author} • {formatDate(commit.date)}
                </p>
              </div>
            </div>
          ))}

          {history.length === 0 && (
            <div className="text-center py-8 text-gray-500">
              No commits yet
            </div>
          )}
        </div>
      </div>

      {/* Webhook Info */}
      <div className="bg-gray-800 rounded-lg p-6 mt-6">
        <h2 className="text-lg font-semibold text-white mb-4">Webhook URLs</h2>
        <p className="text-gray-400 text-sm mb-4">
          Configure your Git provider to send push events to these URLs for automatic sync.
        </p>

        <div className="space-y-3">
          <div>
            <label className="text-sm text-gray-400">GitHub</label>
            <code className="block mt-1 p-2 bg-gray-700 rounded text-sm text-gray-300 font-mono">
              {window.location.origin}/api/v1/webhook/github
            </code>
          </div>
          <div>
            <label className="text-sm text-gray-400">GitLab</label>
            <code className="block mt-1 p-2 bg-gray-700 rounded text-sm text-gray-300 font-mono">
              {window.location.origin}/api/v1/webhook/gitlab
            </code>
          </div>
          <div>
            <label className="text-sm text-gray-400">Generic (manual trigger)</label>
            <code className="block mt-1 p-2 bg-gray-700 rounded text-sm text-gray-300 font-mono">
              POST {window.location.origin}/api/v1/webhook/generic
            </code>
          </div>
        </div>
      </div>
    </div>
  )
}
