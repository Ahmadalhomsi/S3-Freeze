import { useState } from 'react'
import { useNavigate } from 'react-router'
import { useMutation, useQuery } from '@tanstack/react-query'
import { ArchiveRestore, FolderInput, Plus, RotateCcw, TriangleAlert } from 'lucide-react'
import { toast } from 'sonner'
import { api, type Job, type RestoreRequest, type Storage } from '@/lib/api'
import { cn, formatDate, slug } from '@/lib/utils'
import { useBuckets } from '@/lib/hooks'
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
import { LocationPicker, validBucketName, type Location } from '@/components/LocationPicker'

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
  const [mode, setMode] = useState<'original' | 'new' | 'other'>('original')
  const [target, setTarget] = useState<Location>({ storage_id: job.source_storage_id, bucket: job.source_bucket, prefix: '' })
  // e.g. "photos-restored-2026-10-07": valid as an S3 bucket name.
  const newBucketName = `${slug(job.source_bucket || job.name).replace(/[_.]+/g, '-').slice(0, 40) || 'restore'}-restored-${localDate()}`
  const choose = (m: typeof mode) => {
    setMode(m)
    if (m === 'new') setTarget({ storage_id: job.source_storage_id, bucket: newBucketName, prefix: '' })
    if (m === 'other') setTarget({ storage_id: job.source_storage_id, bucket: job.source_bucket, prefix: '' })
  }
  const targetStorage = storages.find((s) => s.id === target.storage_id)
  const targetBuckets = useBuckets(target.storage_id)
  // Only names of buckets that will be created need to follow S3 naming rules.
  const badBucket =
    mode !== 'original' &&
    targetStorage?.type === 's3' &&
    !!target.bucket &&
    !(targetBuckets.data ?? []).includes(target.bucket) &&
    !validBucketName(target.bucket)
  const wholeStorage = job.source_bucket === ''
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

        <div className="grid gap-3 sm:grid-cols-3">
          {(
            [
              ['original', RotateCcw, 'Original location', wholeStorage ? 'Every object back into its own bucket' : `Back into ${job.source_bucket}`],
              ['new', Plus, 'New bucket', 'Restore side by side into a fresh bucket'],
              ['other', FolderInput, 'Other location', 'Any storage, existing bucket or folder'],
            ] as const
          ).map(([value, Icon, title, desc]) => (
            <button
              key={value}
              type="button"
              onClick={() => choose(value)}
              className={cn(
                'flex flex-col items-start gap-1 rounded-lg border p-3 text-left transition-colors',
                mode === value ? 'border-primary bg-accent/60 ring-1 ring-primary' : 'hover:bg-muted',
              )}
            >
              <span className="flex items-center gap-1.5 text-sm font-medium">
                <Icon className={cn('size-4', mode === value ? 'text-primary' : 'text-muted-foreground')} />
                {title}
              </span>
              <span className="text-xs text-muted-foreground">{desc}</span>
            </button>
          ))}
        </div>

        {mode !== 'original' && (
          <LocationPicker
            key={mode}
            storages={storages}
            value={target}
            onChange={setTarget}
            newBucketSuggestion={newBucketName}
            bucketRequired={wholeStorage ? false : undefined}
            bucketHint={wholeStorage ? 'Leave empty to restore each bucket under its original name (missing buckets are created).' : undefined}
            folderLabel="Into folder"
            folderHint="Prepended to every restored key, e.g. restored-2024-05-01/."
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
          <Button loading={restore.isPending} disabled={badBucket} onClick={() => restore.mutate()}>
            <ArchiveRestore /> Start restore
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Today's date in the viewer's time zone, as YYYY-MM-DD. */
function localDate(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
