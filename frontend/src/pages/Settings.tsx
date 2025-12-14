import { useQuery } from '@tanstack/react-query'
import { GitBranch, RefreshCw, Clock, Copy, Check } from 'lucide-react'
import { getGitStatus, getGitHistory, syncGit } from '../services/api'
import { useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'

export default function Settings() {
  const [isSyncing, setIsSyncing] = useState(false)
  const [copiedUrl, setCopiedUrl] = useState<string | null>(null)

  const { data: gitStatus, refetch: refetchStatus, isLoading: statusLoading } = useQuery({
    queryKey: ['gitStatus'],
    queryFn: getGitStatus,
  })

  const { data: history = [], refetch: refetchHistory, isLoading: historyLoading } = useQuery({
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
    } finally {
      setIsSyncing(false)
    }
  }

  const copyToClipboard = async (text: string, id: string) => {
    await navigator.clipboard.writeText(text)
    setCopiedUrl(id)
    setTimeout(() => setCopiedUrl(null), 2000)
  }

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr)
    return date.toLocaleString()
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Settings</h1>
        <p className="text-muted-foreground">Git synchronization and system configuration</p>
      </div>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="p-2 bg-muted rounded-lg">
              <GitBranch className="h-5 w-5" />
            </div>
            <div>
              <CardTitle>Git Status</CardTitle>
              {statusLoading ? (
                <Skeleton className="h-4 w-32 mt-1" />
              ) : (
                <CardDescription>
                  {gitStatus?.clean ? (
                    <span className="text-success">Working tree clean</span>
                  ) : (
                    <span className="text-warning">
                      {gitStatus?.changed_files.length || 0} uncommitted changes
                    </span>
                  )}
                </CardDescription>
              )}
            </div>
          </div>
          <Button onClick={handleSync} disabled={isSyncing}>
            <RefreshCw className={`h-4 w-4 mr-2 ${isSyncing ? 'animate-spin' : ''}`} />
            {isSyncing ? 'Syncing...' : 'Sync from Remote'}
          </Button>
        </CardHeader>
        {gitStatus?.changed_files && gitStatus.changed_files.length > 0 && (
          <CardContent>
            <div className="p-3 bg-muted rounded-lg">
              <h3 className="text-sm font-medium mb-2">Changed files:</h3>
              <ul className="space-y-1">
                {gitStatus.changed_files.map((file) => (
                  <li key={file} className="text-sm font-mono text-muted-foreground">
                    {file}
                  </li>
                ))}
              </ul>
            </div>
          </CardContent>
        )}
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-3">
            <div className="p-2 bg-muted rounded-lg">
              <Clock className="h-5 w-5" />
            </div>
            <div>
              <CardTitle>Recent Commits</CardTitle>
              <CardDescription>Latest changes in the repository</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {historyLoading ? (
            <div className="space-y-3">
              {[...Array(5)].map((_, i) => (
                <Skeleton key={i} className="h-16 w-full" />
              ))}
            </div>
          ) : history.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">No commits yet</div>
          ) : (
            <div className="space-y-3">
              {history.map((commit, index) => (
                <div key={commit.hash}>
                  {index > 0 && <Separator className="my-3" />}
                  <div className="flex items-start gap-3">
                    <code className="text-sm text-primary font-mono">{commit.hash}</code>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm truncate">
                        {commit.message.startsWith('[state-snapshot]') ? (
                          <>
                            <Badge variant="secondary" className="mr-2 text-xs">
                              Auto Snapshot
                            </Badge>
                            {commit.message.replace('[state-snapshot]', '').trim()}
                          </>
                        ) : (
                          commit.message
                        )}
                      </p>
                      <p className="text-xs text-muted-foreground mt-1">
                        {commit.author} - {formatDate(commit.date)}
                      </p>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Webhook URLs</CardTitle>
          <CardDescription>
            Configure your Git provider to send push events to these URLs for automatic sync.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <WebhookUrl
            label="GitHub"
            url={`${window.location.origin}/api/v1/webhook/github`}
            copied={copiedUrl === 'github'}
            onCopy={() => copyToClipboard(`${window.location.origin}/api/v1/webhook/github`, 'github')}
          />
          <Separator />
          <WebhookUrl
            label="GitLab"
            url={`${window.location.origin}/api/v1/webhook/gitlab`}
            copied={copiedUrl === 'gitlab'}
            onCopy={() => copyToClipboard(`${window.location.origin}/api/v1/webhook/gitlab`, 'gitlab')}
          />
          <Separator />
          <WebhookUrl
            label="Generic (manual trigger)"
            url={`POST ${window.location.origin}/api/v1/webhook/generic`}
            copied={copiedUrl === 'generic'}
            onCopy={() => copyToClipboard(`${window.location.origin}/api/v1/webhook/generic`, 'generic')}
          />
        </CardContent>
      </Card>
    </div>
  )
}

function WebhookUrl({
  label,
  url,
  copied,
  onCopy,
}: {
  label: string
  url: string
  copied: boolean
  onCopy: () => void
}) {
  return (
    <div className="space-y-2">
      <label className="text-sm font-medium">{label}</label>
      <div className="flex items-center gap-2">
        <code className="flex-1 p-2 bg-muted rounded text-sm font-mono overflow-x-auto">{url}</code>
        <Button variant="outline" size="icon" onClick={onCopy}>
          {copied ? <Check className="h-4 w-4 text-success" /> : <Copy className="h-4 w-4" />}
        </Button>
      </div>
    </div>
  )
}
