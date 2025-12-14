import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { HardDrive, Trash2, Plus, Download, Search } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getVolumes, deleteVolume, backupVolume, createVolume } from '../services/api'
import type { Volume } from '../types'
import { formatBytes } from '@/lib/utils'

export default function Volumes() {
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [volumeToDelete, setVolumeToDelete] = useState<Volume | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const queryClient = useQueryClient()

  const { data: volumes = [], isLoading } = useQuery({
    queryKey: ['volumes'],
    queryFn: getVolumes,
  })

  const deleteMutation = useMutation({
    mutationFn: deleteVolume,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['volumes'] })
      setDeleteDialogOpen(false)
      setVolumeToDelete(null)
    },
  })

  const backupMutation = useMutation({
    mutationFn: backupVolume,
  })

  const handleDeleteClick = (volume: Volume) => {
    setVolumeToDelete(volume)
    setDeleteDialogOpen(true)
  }

  const handleConfirmDelete = () => {
    if (volumeToDelete) {
      deleteMutation.mutate(volumeToDelete.name)
    }
  }

  const filteredVolumes = volumes.filter((volume) =>
    volume.name.toLowerCase().includes(searchQuery.toLowerCase())
  )

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold">Volumes</h1>
          <p className="text-muted-foreground">{volumes.length} volumes</p>
        </div>
        <Button onClick={() => setShowCreateDialog(true)}>
          <Plus className="h-4 w-4 mr-2" />
          Create Volume
        </Button>
      </div>

      <div className="relative max-w-sm">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          placeholder="Search volumes..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="pl-9"
        />
      </div>

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {[...Array(6)].map((_, i) => (
            <Skeleton key={i} className="h-48" />
          ))}
        </div>
      ) : filteredVolumes.length === 0 ? (
        <div className="text-center py-12 text-muted-foreground">
          {searchQuery ? 'No volumes match your search' : 'No volumes found. Create one to get started.'}
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {filteredVolumes.map((volume) => (
            <Card key={volume.name}>
              <CardContent className="pt-6">
                <div className="flex items-start justify-between mb-4">
                  <div className="flex items-center gap-3">
                    <div className="p-2 bg-info/10 rounded-lg text-info">
                      <HardDrive className="h-6 w-6" />
                    </div>
                    <div>
                      <h3 className="font-medium">{volume.name}</h3>
                      <p className="text-sm text-muted-foreground">{volume.driver}</p>
                    </div>
                  </div>
                  {volume.is_managed && (
                    <Badge variant="outline">managed</Badge>
                  )}
                </div>

                <div className="space-y-2 text-sm mb-4">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Size</span>
                    <span>{formatBytes(volume.size_bytes || 0)}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Mountpoint</span>
                    <span
                      className="font-mono text-xs truncate max-w-[180px]"
                      title={volume.mountpoint}
                    >
                      {volume.mountpoint}
                    </span>
                  </div>
                </div>

                <div className="flex items-center gap-2 pt-4 border-t">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => backupMutation.mutate(volume.name)}
                    disabled={backupMutation.isPending}
                  >
                    <Download className="h-4 w-4 mr-2" />
                    Backup
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive hover:text-destructive"
                    onClick={() => handleDeleteClick(volume)}
                  >
                    <Trash2 className="h-4 w-4 mr-2" />
                    Delete
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <CreateVolumeDialog open={showCreateDialog} onOpenChange={setShowCreateDialog} />

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Volume</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete &quot;{volumeToDelete?.name}&quot;? This action cannot be undone and all data will be lost.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleConfirmDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function CreateVolumeDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [name, setName] = useState('')
  const [driver, setDriver] = useState('local')
  const queryClient = useQueryClient()

  const createMutation = useMutation({
    mutationFn: () => createVolume(name, driver),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['volumes'] })
      onOpenChange(false)
      setName('')
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (name) {
      createMutation.mutate()
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create Volume</DialogTitle>
          <DialogDescription>Create a new Docker volume for persistent storage.</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <label className="text-sm font-medium">Name</label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="my-volume"
                required
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">Driver</label>
              <Select value={driver} onValueChange={setDriver}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="local">local</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
