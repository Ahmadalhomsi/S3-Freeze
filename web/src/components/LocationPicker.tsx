import { useEffect, useState } from 'react'
import { Cloud, Database, Folder, FolderOpen, Layers, Loader2 } from 'lucide-react'
import type { Storage } from '@/lib/api'
import { useBuckets, useDisk, useFolders, useUsage } from '@/lib/hooks'
import { cn, formatBytes, formatNumber } from '@/lib/utils'
import { Field } from '@/components/ui'
import { Combobox } from '@/components/ui/combobox'

export interface Location {
  storage_id: number
  bucket: string
  prefix: string
}

export function storageIcon(s?: Storage) {
  return s?.type === 'local' ? <Folder /> : <Cloud />
}

interface Props {
  storages: Storage[]
  value: Location
  onChange: (v: Location) => void
  /** Offer "entire storage" vs "one bucket" (backup sources). */
  scope?: boolean
  /** Show the size of the selection. */
  showSize?: boolean
  bucketRequired?: boolean
  bucketHint?: string
  folderLabel?: string
  folderHint?: string
}

export function LocationPicker({
  storages,
  value,
  onChange,
  scope,
  showSize,
  bucketRequired,
  bucketHint,
  folderLabel = 'Folder',
  folderHint,
}: Props) {
  const storage = storages.find((s) => s.id === value.storage_id)
  const isLocal = storage?.type === 'local'
  const [whole, setWhole] = useState(scope ? value.bucket === '' && value.prefix === '' : false)
  const buckets = useBuckets(value.storage_id)
  const showBucket = !whole
  const folders = useFolders(value.storage_id, value.bucket, value.prefix, showBucket && (isLocal || !!value.bucket))

  const set = (patch: Partial<Location>) => onChange({ ...value, ...patch })

  // Pick the first bucket when one is required and none is chosen yet.
  useEffect(() => {
    if (!whole && !isLocal && bucketRequired !== false && !value.bucket && buckets.data?.length) {
      set({ bucket: buckets.data[0] })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [whole, buckets.data, isLocal])

  return (
    <div className="flex flex-col gap-4">
      <Field label="Storage">
        <Combobox
          value={String(value.storage_id || '')}
          onChange={(v) => onChange({ storage_id: Number(v), bucket: '', prefix: '' })}
          allowCustom={false}
          clearable={false}
          placeholder="Select a storage…"
          options={storages.map((s) => ({
            value: String(s.id),
            label: s.name,
            icon: storageIcon(s),
            hint: s.builtin ? 'This server' : s.type === 's3' ? 'S3' : 'Local disk',
          }))}
        />
      </Field>

      {scope && (
        <div className="grid grid-cols-2 gap-1 rounded-lg bg-muted p-1">
          {[
            { v: true, icon: Layers, label: isLocal ? 'Entire directory' : 'Entire storage', sub: isLocal ? 'Every file and folder' : 'All buckets' },
            { v: false, icon: Database, label: isLocal ? 'One folder' : 'One bucket', sub: isLocal ? 'Pick a subfolder' : 'Optionally a folder in it' },
          ].map((o) => (
            <button
              key={o.label}
              type="button"
              onClick={() => {
                setWhole(o.v)
                if (o.v) set({ bucket: '', prefix: '' })
              }}
              className={cn(
                'flex items-center gap-2.5 rounded-md px-3 py-2 text-left transition-colors',
                whole === o.v ? 'bg-card shadow-sm' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              <o.icon className={cn('size-4 shrink-0', whole === o.v && 'text-primary')} />
              <span className="min-w-0">
                <span className="block text-sm font-medium">{o.label}</span>
                <span className="block truncate text-xs text-muted-foreground">{o.sub}</span>
              </span>
            </button>
          ))}
        </div>
      )}

      {showBucket && (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label={isLocal ? 'Subfolder' : 'Bucket'}
            hint={buckets.error ? 'Could not list buckets — type the name.' : bucketHint ?? (isLocal ? 'Optional' : undefined)}
          >
            <Combobox
              value={value.bucket}
              onChange={(v) => set({ bucket: v, prefix: '' })}
              loading={buckets.isFetching}
              placeholder={isLocal ? 'Optional subfolder…' : 'Select or type a bucket…'}
              required={bucketRequired ?? !isLocal}
              icon={<Database />}
              emptyText={isLocal ? 'No subfolders' : 'No buckets'}
              options={(buckets.data ?? []).map((b) => ({ value: b, icon: <Database /> }))}
            />
          </Field>
          <Field label={folderLabel} hint={folderHint}>
            <Combobox
              value={value.prefix}
              onChange={(v) => set({ prefix: v })}
              loading={folders.isFetching}
              placeholder="Optional, e.g. photos/2024/"
              icon={<FolderOpen />}
              emptyText="No subfolders here"
              options={(folders.data ?? []).map((f) => ({ value: f, icon: <Folder /> }))}
            />
          </Field>
        </div>
      )}

      {showSize && (whole || isLocal || value.bucket) && <SizeEstimate location={value} />}
    </div>
  )
}

export function SizeEstimate({ location, className }: { location: Location; className?: string }) {
  const [debounced, setDebounced] = useState(location)
  useEffect(() => {
    const t = setTimeout(() => setDebounced(location), 500)
    return () => clearTimeout(t)
  }, [location.storage_id, location.bucket, location.prefix]) // eslint-disable-line react-hooks/exhaustive-deps
  const usage = useUsage(debounced.storage_id, debounced.bucket, debounced.prefix)

  return (
    <div className={cn('flex items-center gap-2 rounded-lg border border-dashed px-3 py-2 text-sm', className)}>
      <Layers className="size-4 shrink-0 text-muted-foreground" />
      {usage.isFetching ? (
        <span className="flex items-center gap-2 text-muted-foreground">
          <Loader2 className="size-3.5 animate-spin" /> Calculating size…
        </span>
      ) : usage.error ? (
        <span className="text-muted-foreground">Size unavailable: {usage.error.message}</span>
      ) : usage.data ? (
        <span>
          <span className="font-medium tabular-nums">{formatBytes(usage.data.size)}</span>
          <span className="text-muted-foreground"> · {formatNumber(usage.data.objects)} objects</span>
          {usage.data.partial && <span className="text-warning"> (at least — listing timed out)</span>}
        </span>
      ) : (
        <span className="text-muted-foreground">Select a location to see its size</span>
      )}
    </div>
  )
}

export function DiskSpace({ storageId, className }: { storageId: number; className?: string }) {
  const disk = useDisk(storageId)
  if (disk.isLoading) return <p className={cn('text-xs text-muted-foreground', className)}>Checking free space…</p>
  if (!disk.data) return null
  const used = disk.data.total - disk.data.free
  const pct = disk.data.total ? Math.round((used / disk.data.total) * 100) : 0
  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div className={cn('h-full rounded-full', pct > 90 ? 'bg-destructive' : pct > 75 ? 'bg-warning' : 'bg-success')} style={{ width: `${pct}%` }} />
      </div>
      <p className="text-xs text-muted-foreground tabular-nums">
        <span className="font-medium text-foreground">{formatBytes(disk.data.free)} free</span> of {formatBytes(disk.data.total)}
      </p>
    </div>
  )
}
