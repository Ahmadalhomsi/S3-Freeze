import { useEffect, useRef } from 'react'
import { Link, useParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Square } from 'lucide-react'
import { toast } from 'sonner'
import { api, type Run } from '@/lib/api'
import { formatBytes, formatDate, formatDuration, formatNumber } from '@/lib/utils'
import { Button, Card, CardContent, CardHeader, CardTitle } from '@/components/ui'
import { ErrorBox, kindLabel, Loading, PageHeader, RunProgress, StatusBadge } from '@/components/common'

export default function RunDetailPage() {
  const { id } = useParams()
  const qc = useQueryClient()
  const logRef = useRef<HTMLPreElement>(null)
  const stick = useRef(true)

  const { data: run, isLoading, error } = useQuery({
    queryKey: ['run', id],
    queryFn: () => api.get<Run>(`/api/runs/${id}`),
    refetchInterval: (q) => (q.state.data?.status === 'running' ? 1500 : false),
  })

  const cancel = useMutation({
    mutationFn: () => api.post(`/api/runs/${id}/cancel`),
    onSuccess: () => {
      toast.info('Cancelling…')
      qc.invalidateQueries()
    },
    onError: (e) => toast.error(e.message),
  })

  useEffect(() => {
    const el = logRef.current
    if (el && stick.current) el.scrollTop = el.scrollHeight
  }, [run?.log])

  if (isLoading) return <Loading />
  if (error || !run) return <ErrorBox error={error} />

  const stats: [string, string][] = [
    ['Started', formatDate(run.started_at)],
    ['Duration', formatDuration(run.started_at, run.finished_at)],
    ['Objects', `${formatNumber(run.objects_done)} / ${formatNumber(run.objects_total)}`],
    ['Data processed', formatBytes(run.bytes_done)],
  ]
  if (run.kind === 'backup') stats.push(['Uploaded (after dedup)', formatBytes(run.bytes_uploaded)])
  if (run.objects_failed) stats.push(['Failed objects', formatNumber(run.objects_failed)])

  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            {kindLabel[run.kind] ?? run.kind} #{run.id} <StatusBadge status={run.status} />
          </span>
        }
        description={run.detail}
        actions={
          run.status === 'running' && (
            <Button variant="destructive" loading={cancel.isPending} onClick={() => cancel.mutate()}>
              <Square /> Cancel
            </Button>
          )
        }
      >
        {run.job_id ? (
          <Link to={`/jobs/${run.job_id}`} className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeft className="size-4" /> {run.job_name}
          </Link>
        ) : null}
      </PageHeader>

      <div className="flex flex-col gap-6">
        {run.error && run.status !== 'cancelled' && <ErrorBox error={run.error} />}

        <Card>
          <CardContent className="flex flex-col gap-5 pt-5">
            <RunProgress run={run} />
            <dl className="grid grid-cols-2 gap-4 text-sm md:grid-cols-3 lg:grid-cols-6">
              {stats.map(([k, v]) => (
                <div key={k}>
                  <dt className="text-xs text-muted-foreground">{k}</dt>
                  <dd className="mt-0.5 font-medium tabular-nums">{v}</dd>
                </div>
              ))}
            </dl>
            {run.snapshot_id && (
              <p className="text-sm">
                Created snapshot{' '}
                <Link to={`/jobs/${run.job_id}/snapshots/${run.snapshot_id}`} className="font-mono text-primary hover:underline">
                  {run.snapshot_id}
                </Link>
              </p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Log</CardTitle>
          </CardHeader>
          <CardContent>
            <pre
              ref={logRef}
              onScroll={(e) => {
                const el = e.currentTarget
                stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
              }}
              className="max-h-[60vh] overflow-auto rounded-lg bg-muted p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all"
            >
              {run.log || 'Waiting for output…'}
            </pre>
          </CardContent>
        </Card>
      </div>
    </>
  )
}
