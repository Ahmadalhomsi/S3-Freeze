import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Database, Layers, Loader2, Pencil, RefreshCw, Zap } from 'lucide-react'
import { useBuckets, useStorages, useUsage } from '@/lib/hooks'
import { formatBytes, formatNumber } from '@/lib/utils'
import { Badge, Button, Card, CardHeader, CardTitle, Table, Td, Th, Tr } from '@/components/ui'
import { EmptyState, ErrorBox, Loading, PageHeader, Stat } from '@/components/common'
import { useQuickBackup } from '@/components/QuickBackup'
import { StorageDialog } from '@/pages/Storages'

// Sizes are calculated automatically up to this many buckets.
const AUTO_SIZE_LIMIT = 30

export default function StorageDetailPage() {
  const id = Number(useParams().id)
  const qc = useQueryClient()
  const quick = useQuickBackup()
  const [editing, setEditing] = useState(false)
  const { data: storages, isLoading } = useStorages()
  const storage = storages?.find((s) => s.id === id)
  const buckets = useBuckets(id)
  const total = useUsage(id, '', '', !!storage)

  if (isLoading) return <Loading />
  if (!storage) return <ErrorBox error="Storage not found" />
  const isLocal = storage.type === 'local'
  const names = buckets.data ?? []

  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            {storage.name}
            <Badge variant="outline">{isLocal ? 'Local disk' : 'S3-compatible'}</Badge>
          </span>
        }
        description={<span className="font-mono">{isLocal ? storage.local_path : storage.endpoint}</span>}
        actions={
          <>
            <Button variant="outline" onClick={() => setEditing(true)}>
              <Pencil /> Edit
            </Button>
            {!storage.builtin && (
              <Button onClick={() => quick({ storage_id: id, bucket: '', prefix: '' })}>
                <Zap /> Back up entire storage
              </Button>
            )}
          </>
        }
      >
        <Link to="/storages" className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" /> Storages
        </Link>
      </PageHeader>

      <div className="flex flex-col gap-6">
        <div className="grid gap-4 sm:grid-cols-3">
          <Stat
            icon={Layers}
            label="Total size"
            value={total.isFetching ? <Loader2 className="size-6 animate-spin text-muted-foreground" /> : formatBytes(total.data?.size)}
            sub={total.data?.partial ? 'At least — listing timed out' : undefined}
          />
          <Stat icon={Database} label="Objects" value={total.isFetching ? '…' : formatNumber(total.data?.objects)} />
          <Stat icon={Database} label={isLocal ? 'Folders' : 'Buckets'} value={buckets.isLoading ? '…' : formatNumber(names.length)} />
        </div>

        <Card>
          <CardHeader className="flex-row items-center justify-between">
            <CardTitle>{isLocal ? 'Top-level folders' : 'Buckets'}</CardTitle>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                qc.invalidateQueries({ queryKey: ['buckets', id] })
                qc.invalidateQueries({ queryKey: ['usage', id] })
              }}
            >
              <RefreshCw /> Refresh
            </Button>
          </CardHeader>
          {buckets.isLoading ? (
            <Loading />
          ) : buckets.error ? (
            <div className="p-5 pt-0">
              <ErrorBox error={buckets.error} />
            </div>
          ) : !names.length ? (
            <EmptyState icon={Database} title={isLocal ? 'No folders yet' : 'No buckets'} />
          ) : (
            <Table>
              <thead>
                <tr>
                  <Th>Name</Th>
                  <Th className="text-right">Objects</Th>
                  <Th className="text-right">Size</Th>
                  <Th className="w-32" />
                </tr>
              </thead>
              <tbody>
                {names.map((b) => (
                  <BucketRow key={b} storageId={id} bucket={b} auto={names.length <= AUTO_SIZE_LIMIT} onBackup={storage.builtin ? undefined : () => quick({ storage_id: id, bucket: b, prefix: '' })} />
                ))}
              </tbody>
            </Table>
          )}
        </Card>
      </div>

      {editing && <StorageDialog storage={storage} onClose={() => setEditing(false)} onSaved={() => qc.invalidateQueries()} />}
    </>
  )
}

function BucketRow({ storageId, bucket, auto, onBackup }: { storageId: number; bucket: string; auto: boolean; onBackup?: () => void }) {
  const [requested, setRequested] = useState(auto)
  const usage = useUsage(storageId, bucket, '', requested)
  return (
    <Tr>
      <Td>
        <span className="flex items-center gap-2 font-medium">
          <Database className="size-4 text-primary" /> {bucket}
        </span>
      </Td>
      {!requested ? (
        <Td colSpan={2} className="text-right">
          <Button variant="ghost" size="sm" onClick={() => setRequested(true)}>
            Calculate size
          </Button>
        </Td>
      ) : usage.isFetching ? (
        <Td colSpan={2} className="text-right text-muted-foreground">
          <Loader2 className="ml-auto size-4 animate-spin" />
        </Td>
      ) : usage.error ? (
        <Td colSpan={2} className="text-right text-xs text-destructive">
          {usage.error.message}
        </Td>
      ) : (
        <>
          <Td className="text-right tabular-nums">{formatNumber(usage.data?.objects)}</Td>
          <Td className="text-right tabular-nums">
            {formatBytes(usage.data?.size)}
            {usage.data?.partial && <span className="text-warning">+</span>}
          </Td>
        </>
      )}
      <Td className="text-right">
        {onBackup && (
          <Button variant="outline" size="sm" onClick={onBackup}>
            <Zap /> Back up
          </Button>
        )}
      </Td>
    </Tr>
  )
}
