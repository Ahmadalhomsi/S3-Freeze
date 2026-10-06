import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArchiveRestore, ArrowLeft, FolderOpen, History, Pencil, Play, RefreshCw, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { api, type Job, type Run, type Snapshot, type Storage } from '@/lib/api'
import { cn, formatBytes, formatDate, formatNumber, timeAgo } from '@/lib/utils'
import { Button, buttonVariants, Card, CardContent, CardHeader, CardTitle, Table, Td, Th, Tr } from '@/components/ui'
import { ConfirmDialog, EmptyState, ErrorBox, HealthBadge, Loading, PageHeader, RunProgress } from '@/components/common'
import { RunsTable, useRunJob } from '@/components/tables'
import { RestoreDialog } from '@/components/RestoreDialog'

export default function JobDetailPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [tab, setTab] = useState<'snapshots' | 'activity'>('snapshots')
  const [restoring, setRestoring] = useState<Snapshot | null>(null)
  const [deletingSnap, setDeletingSnap] = useState<Snapshot | null>(null)
  const [deletingJob, setDeletingJob] = useState(false)
  const runJob = useRunJob()

  const job = useQuery({
    queryKey: ['job', id],
    queryFn: () => api.get<Job>(`/api/jobs/${id}`),
    refetchInterval: (q) => (q.state.data?.active_run_id ? 2000 : 15000),
  })
  const active = job.data?.active_run_id ?? 0
  const snapshots = useQuery({
    queryKey: ['snapshots', id, active],
    queryFn: () => api.get<Snapshot[]>(`/api/jobs/${id}/snapshots`),
  })
  const runs = useQuery({
    queryKey: ['runs', { job: id }, active],
    queryFn: () => api.get<Run[]>(`/api/runs?job_id=${id}&limit=50`),
    refetchInterval: active ? 2000 : false,
  })
  const activeRun = runs.data?.find((r) => r.id === active)
  const { data: storages = [] } = useQuery({ queryKey: ['storages'], queryFn: () => api.get<Storage[]>('/api/storages') })

  const scan = useMutation({
    mutationFn: () => api.post<{ run_id: number }>(`/api/jobs/${id}/scan`),
    onSuccess: () => {
      toast.success('Repository scan started')
      qc.invalidateQueries()
    },
    onError: (e) => toast.error(e.message),
  })

  if (job.isLoading) return <Loading />
  if (job.error || !job.data) return <ErrorBox error={job.error} />
  const j = job.data
  const storageName = (sid: number) => storages.find((s) => s.id === sid)?.name ?? `#${sid}`

  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            {j.name} <HealthBadge health={j.health} />
          </span>
        }
        actions={
          <>
            <Button variant="outline" size="icon" title="Delete job" onClick={() => setDeletingJob(true)}>
              <Trash2 />
            </Button>
            <Link to={`/jobs/${j.id}/edit`} className={buttonVariants({ variant: 'outline' })}>
              <Pencil /> Edit
            </Link>
            <Button onClick={() => runJob.mutate(j.id)} loading={runJob.isPending} disabled={!!active}>
              <Play /> Run backup now
            </Button>
          </>
        }
      >
        <Link to="/jobs" className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" /> Backup jobs
        </Link>
      </PageHeader>

      <div className="flex flex-col gap-6">
        {activeRun && (
          <Card>
            <CardHeader className="flex-row items-center justify-between">
              <CardTitle className="text-base">{activeRun.detail}</CardTitle>
              <Link to={`/runs/${activeRun.id}`} className={buttonVariants({ variant: 'outline', size: 'sm' })}>
                View log
              </Link>
            </CardHeader>
            <CardContent>
              <RunProgress run={activeRun} />
            </CardContent>
          </Card>
        )}

        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Info label="Source" value={`${j.source_bucket || '/'}${j.source_prefix ? `/${j.source_prefix}` : ''}`} sub={storageName(j.source_storage_id)} />
          <Info label="Destination" value={`${j.dest_bucket || '/'}${j.dest_prefix ? `/${j.dest_prefix}` : ''}`} sub={storageName(j.dest_storage_id)} />
          <Info
            label="Schedule"
            value={j.schedule || 'Manual'}
            mono={!!j.schedule}
            sub={!j.enabled ? 'Paused' : j.next_run ? `Next ${timeAgo(j.next_run)}` : 'Not scheduled'}
          />
          <Info
            label="Retention"
            value={[j.keep_last ? `Last ${j.keep_last}` : '', j.keep_days ? `${j.keep_days} days` : ''].filter(Boolean).join(' + ') || 'Keep all'}
            sub={[j.encryption ? 'Encrypted' : 'Not encrypted', j.compression ? 'compressed' : 'uncompressed'].join(', ')}
          />
        </div>

        <Card>
          <div className="flex items-center justify-between gap-2 border-b px-5 pt-3">
            <div className="flex gap-4">
              {(['snapshots', 'activity'] as const).map((t) => (
                <button
                  key={t}
                  onClick={() => setTab(t)}
                  className={cn(
                    '-mb-px border-b-2 pb-3 text-sm font-medium capitalize transition-colors',
                    tab === t ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground',
                  )}
                >
                  {t}
                  {t === 'snapshots' && snapshots.data && <span className="ml-1.5 text-muted-foreground">{snapshots.data.length}</span>}
                </button>
              ))}
            </div>
            {tab === 'snapshots' && (
              <Button
                variant="ghost"
                size="sm"
                className="mb-2"
                title="Import snapshots that exist in the repository but not in this dashboard (e.g. after reinstalling)"
                loading={scan.isPending}
                disabled={!!active}
                onClick={() => scan.mutate()}
              >
                <RefreshCw /> Scan repository
              </Button>
            )}
          </div>

          {tab === 'snapshots' ? (
            snapshots.isLoading ? (
              <Loading />
            ) : !snapshots.data?.length ? (
              <EmptyState
                icon={History}
                title="No snapshots yet"
                description="Run a backup to create the first snapshot. If this repository already has snapshots, use “Scan repository”."
              />
            ) : (
              <Table>
                <thead>
                  <tr>
                    <Th>Snapshot</Th>
                    <Th className="text-right">Objects</Th>
                    <Th className="text-right">Size</Th>
                    <Th className="hidden md:table-cell text-right">New data</Th>
                    <Th className="text-right">Actions</Th>
                  </tr>
                </thead>
                <tbody>
                  {snapshots.data.map((s, i) => (
                    <Tr key={s.id}>
                      <Td>
                        <div className="flex items-center gap-2 font-medium">
                          {formatDate(s.created_at)}
                          {i === 0 && (
                            <span className="rounded-full bg-accent px-1.5 py-0.5 text-[10px] font-semibold uppercase text-accent-foreground">
                              Latest
                            </span>
                          )}
                        </div>
                        <div className="font-mono text-xs text-muted-foreground">{s.id}</div>
                      </Td>
                      <Td className="text-right tabular-nums">{formatNumber(s.objects)}</Td>
                      <Td className="text-right tabular-nums">{formatBytes(s.size)}</Td>
                      <Td className="hidden md:table-cell text-right tabular-nums text-muted-foreground">{formatBytes(s.added_bytes)}</Td>
                      <Td>
                        <div className="flex justify-end gap-1">
                          <Button variant="ghost" size="sm" onClick={() => navigate(`/jobs/${j.id}/snapshots/${s.id}`)}>
                            <FolderOpen /> <span className="hidden sm:inline">Browse</span>
                          </Button>
                          <Button variant="ghost" size="sm" disabled={!!active} onClick={() => setRestoring(s)}>
                            <ArchiveRestore /> <span className="hidden sm:inline">Restore</span>
                          </Button>
                          <Button variant="ghost" size="icon-sm" title="Delete snapshot" disabled={!!active} onClick={() => setDeletingSnap(s)}>
                            <Trash2 />
                          </Button>
                        </div>
                      </Td>
                    </Tr>
                  ))}
                </tbody>
              </Table>
            )
          ) : runs.data?.length ? (
            <RunsTable runs={runs.data} showJob={false} />
          ) : (
            <EmptyState icon={History} title="No runs yet" />
          )}
        </Card>
      </div>

      {restoring && <RestoreDialog job={j} snapshot={restoring} paths={[]} onClose={() => setRestoring(null)} />}

      <ConfirmDialog
        open={!!deletingSnap}
        onOpenChange={(v) => !v && setDeletingSnap(null)}
        title="Delete snapshot?"
        description={
          <>
            The snapshot from <strong>{formatDate(deletingSnap?.created_at)}</strong> will be removed and data no other snapshot uses
            will be deleted from the repository. This cannot be undone.
          </>
        }
        confirmLabel="Delete snapshot"
        destructive
        onConfirm={async () => {
          try {
            await api.del(`/api/jobs/${j.id}/snapshots/${deletingSnap!.id}`)
            toast.success('Deleting snapshot…')
            qc.invalidateQueries()
          } catch (e) {
            toast.error((e as Error).message)
          }
        }}
      />

      <ConfirmDialog
        open={deletingJob}
        onOpenChange={setDeletingJob}
        title={`Delete job “${j.name}”?`}
        description="The job, its history and its snapshot list are removed from the dashboard. Backup data in the destination repository is NOT deleted — you can re-import it later with “Scan repository” from a new job pointing to the same destination."
        confirmLabel="Delete job"
        destructive
        onConfirm={async () => {
          try {
            await api.del(`/api/jobs/${j.id}`)
            toast.success('Job deleted')
            qc.invalidateQueries()
            navigate('/jobs')
          } catch (e) {
            toast.error((e as Error).message)
          }
        }}
      />
    </>
  )
}

function Info({ label, value, sub, mono }: { label: string; value: string; sub?: string; mono?: boolean }) {
  return (
    <div className="min-w-0 rounded-xl border bg-card p-4 shadow-xs">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className={cn('mt-1.5 truncate font-medium', mono && 'font-mono text-sm')} title={value}>
        {value}
      </p>
      {sub && <p className="mt-0.5 truncate text-xs text-muted-foreground">{sub}</p>}
    </div>
  )
}
