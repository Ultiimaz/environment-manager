import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { GitBranch, GitCommit, RefreshCw, Upload, Download, Clock } from "lucide-react"

export default function Git() {
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Git Sync</h1>
          <p className="text-muted-foreground">
            Manage git-driven configuration synchronization
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline">
            <Download className="h-4 w-4 mr-2" />
            Pull Changes
          </Button>
          <Button>
            <Upload className="h-4 w-4 mr-2" />
            Push State
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Branch</CardTitle>
            <GitBranch className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">main</div>
            <p className="text-xs text-muted-foreground">Current branch</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Status</CardTitle>
            <RefreshCw className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <Badge variant="success">Clean</Badge>
            </div>
            <p className="text-xs text-muted-foreground mt-1">No uncommitted changes</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Last Sync</CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">5m ago</div>
            <p className="text-xs text-muted-foreground">Auto-sync enabled</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">Commits</CardTitle>
            <GitCommit className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">0 ahead</div>
            <p className="text-xs text-muted-foreground">0 behind origin</p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recent Commits</CardTitle>
          <CardDescription>
            Latest configuration changes from the repository
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {[
              {
                hash: "a1b2c3d",
                message: "[state-snapshot] Nightly state capture",
                author: "system",
                date: "2 hours ago",
                isSnapshot: true,
              },
              {
                hash: "e4f5g6h",
                message: "Update nginx configuration",
                author: "developer",
                date: "1 day ago",
                isSnapshot: false,
              },
              {
                hash: "i7j8k9l",
                message: "Add new postgres container",
                author: "developer",
                date: "2 days ago",
                isSnapshot: false,
              },
            ].map((commit, index) => (
              <div key={commit.hash}>
                {index > 0 && <Separator className="my-4" />}
                <div className="flex items-start justify-between">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <code className="text-sm font-mono text-muted-foreground">
                        {commit.hash}
                      </code>
                      {commit.isSnapshot && (
                        <Badge variant="secondary">Auto Snapshot</Badge>
                      )}
                    </div>
                    <p className="font-medium">{commit.message}</p>
                    <p className="text-sm text-muted-foreground">
                      {commit.author} - {commit.date}
                    </p>
                  </div>
                  <Button variant="ghost" size="sm">
                    View
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Sync Settings</CardTitle>
          <CardDescription>Configure automatic synchronization behavior</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Auto-pull on changes</p>
              <p className="text-sm text-muted-foreground">
                Automatically pull and apply changes when detected
              </p>
            </div>
            <Badge variant="success">Enabled</Badge>
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Nightly state snapshots</p>
              <p className="text-sm text-muted-foreground">
                Commit current system state every night at 2:00 AM
              </p>
            </div>
            <Badge variant="success">Enabled</Badge>
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Poll interval</p>
              <p className="text-sm text-muted-foreground">
                Check for remote changes every 5 minutes
              </p>
            </div>
            <Badge variant="outline">5m</Badge>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
