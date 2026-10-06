import { useState } from 'react'
import { useNavigate } from 'react-router'
import { useMutation, useQuery } from '@tanstack/react-query'
import { ArchiveRestore, TriangleAlert } from 'lucide-react'
import { toast } from 'sonner'
import { api, type Job, type RestoreRequest, type Storage } from '@/lib/api'
import { cn, formatDate } from '@/lib/utils'
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  SwitchField,
} from '@/components/ui'
import { ErrorBox } from '@/components/common'
import { LocationFields } from '@/pages/JobForm'

export function RestoreDialog({
  job,
  snapshot,
  paths,
  onClose,
}: {
  job: Job
  snapshot: { id: string; created_at: string }
  paths: string[]
  onClose: () => void
}) {
  const navigate = useNavigate()
  const { data: storages = [] } = useQuery({ queryKey: ['storages'], queryFn: () => api.get<Storage[]>('/api/storages') })
  const [mode, setMode] = useState<'original' | 'other'>('original')
  const [target, setTarget] = useState({ storage_id: job.source_storage_id, bucket: job.source_bucket, prefix: '' })
  const [overwrite, setOverwrite] = useState(false)

  const req: RestoreRequest =
    mode === 'original'
      ? { paths, storage_id: job.source_storage_id, bucket: job.source_bucket, prefix: '', strip_prefix: '', overwrite }
      : { paths, ...target, strip_prefix: '', overwrite }

  const restore = useMutation({
    mutationFn: () => api.post<{ run_id: number }>(`/api/jobs/${job.id}/snapshots/${snapshot.id}/restore`, req),
    onSuccess: (r) => {
      toast.success('Restore started')
      onClose()
      navigate(`/runs/${r.run_id}`)
    },
  })

  const what =
    paths.length === 0 ? 'the entire snapshot' : paths.length === 1 ? <code className="font-mono">{paths[0]}</code> : `${paths.length} selected items`

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Restore</DialogTitle>
          <DialogDescription>
            Restore {what} from the snapshot taken {formatDate(snapshot.created_at)}.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-3 sm:grid-cols-2">
          {(
            [
              ['original', 'Original location', `${job.source_bucket || 'local'} — same keys as when backed up`],
              ['other', 'Different location', 'Any storage, bucket and folder'],
            ] as const
          ).map(([value, title, desc]) => (
            <button
              key={value}
              type="button"
              onClick={() => setMode(value)}
              className={cn(
                'flex flex-col items-start gap-1 rounded-lg border p-3 text-left transition-colors',
                mode === value ? 'border-primary bg-accent/60 ring-1 ring-primary' : 'hover:bg-muted',
              )}
            >
              <span className="text-sm font-medium">{title}</span>
              <span className="text-xs text-muted-foreground">{desc}</span>
            </button>
          ))}
        </div>

        {mode === 'other' && (
          <LocationFields
            storages={storages}
            storageId={target.storage_id}
            bucket={target.bucket}
            prefix={target.prefix}
            onStorage={(v) => setTarget((t) => ({ ...t, storage_id: v, bucket: '' }))}
            onBucket={(v) => setTarget((t) => ({ ...t, bucket: v }))}
            onPrefix={(v) => setTarget((t) => ({ ...t, prefix: v }))}
            prefixLabel="Into folder"
            prefixHint="Prepended to every restored key, e.g. restored-2024-05-01/."
          />
        )}

        <div className="rounded-lg border p-4">
          <SwitchField
            label="Overwrite existing objects"
            description="When off, objects that already exist at the target are skipped."
            checked={overwrite}
            onCheckedChange={setOverwrite}
          />
        </div>

        {mode === 'original' && overwrite && (
          <div className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
            <TriangleAlert className="mt-0.5 size-4 shrink-0" />
            Current versions of these objects in the source bucket will be replaced by the snapshot version.
          </div>
        )}

        <ErrorBox error={restore.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={restore.isPending} onClick={() => restore.mutate()}>
            <ArchiveRestore /> Start restore
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
