import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { createProject } from '@/services/api'

interface Props {
  open: boolean
  onClose: () => void
  onCreated: () => void
}

export function AddProjectModal({ open, onClose, onCreated }: Props) {
  const [repoUrl, setRepoUrl] = useState('')
  const [token, setToken] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  async function submit() {
    if (!repoUrl.trim()) return
    setSubmitting(true)
    setError(null)
    try {
      const result = await createProject({
        repo_url: repoUrl.trim(),
        token: token.trim() || undefined,
      })
      onCreated()
      onClose()
      setRepoUrl('')
      setToken('')
      navigate(`/projects/${result.project.id}`)
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'Failed to create project'
      setError(msg)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add a project</DialogTitle>
        </DialogHeader>
        <div className="grid gap-4 py-4">
          <div className="grid gap-2">
            <Label htmlFor="repo-url">Repository URL</Label>
            <Input
              id="repo-url"
              placeholder="https://github.com/u/myapp"
              value={repoUrl}
              onChange={e => setRepoUrl(e.target.value)}
            />
            <p className="text-sm text-muted-foreground">
              Repo must contain a <code>.dev/</code> directory with
              Dockerfile.dev, docker-compose.{'{prod,dev}'}.yml, and config.yaml.
            </p>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="token">GitHub PAT (optional, for private repos)</Label>
            <Input
              id="token"
              type="password"
              placeholder="ghp_..."
              value={token}
              onChange={e => setToken(e.target.value)}
            />
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={submitting || !repoUrl.trim()}>
            {submitting ? 'Cloning...' : 'Add & deploy'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
