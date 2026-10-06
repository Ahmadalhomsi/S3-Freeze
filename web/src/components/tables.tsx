import { Link, useNavigate } from 'react-router'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, Lock, Play } from 'lucide-react'
import { toast } from 'sonner'
import { api, type Job, type Run } from '@/lib/api'
import { formatBytes, formatDuration, formatNumber, timeAgo } from '@/lib/utils'
import { Button, Table, Td, Th, Tr } from '@/components/ui'
import { HealthBadge, kindLabel, StatusBadge } from '@/components/common'

export function useRunJob() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  return useMutation({
    mutationFn: (jobId: number) => api.post<{ run_id: number }>(`/api/jobs/${jobId}/run`),
    onSuccess: (res) => {
      qc.invalidateQueries()
      toast.success('Backup started', {
        action: { label: 'View', onClick: () => navigate(`/runs/${res.run_id}`) },
      })
    },
    onError: (err) => toast.error(err.message),
  })
}

export function JobsTable({ jobs }: { jobs: Job[] }) {
  const runJob = useRunJob()
  return (
    <Table>
      <thead>
        <tr>
          <Th>Job</Th>
          <Th>Status</Th>
          <Th className="hidden md:table-cell">Last backup</Th>
          <Th className="hidden lg:table-cell">Next run</Th>
          <Th className="hidden sm:table-cell text-right">Protected</Th>
          <Th className="w-12" />
        </tr>
      </thead>
      <tbody>
        {jobs.map((j) => (
          <Tr key={j.id}>
            <Td>
              <Link to={`/jobs/${j.id}`} className="group flex flex-col">
                <span className="flex items-center gap-1.5 font-medium group-hover:text-primary">
                  {j.name}
                  {j.encryption && <Lock className="size-3 text-muted-foreground" />}
                </span>
                <span className="flex items-center gap-1 text-xs text-muted-foreground">
                  <span className="max-w-40 truncate">{j.source_bucket || 'local'}{j.source_prefix && `/${j.source_prefix}`}</span>
                  <ArrowRight className="size-3 shrink-0" />
                  <span className="max-w-40 truncate">{j.dest_bucket || 'local'}{j.dest_prefix && `/${j.dest_prefix}`}</span>
                </span>
              </Link>
            </Td>
            <Td>
              <HealthBadge health={j.health} />
            </Td>
            <Td className="hidden md:table-cell text-muted-foreground">
              <span title={j.last_success ?? ''}>{timeAgo(j.last_success)}</span>
            </Td>
            <Td className="hidden lg:table-cell text-muted-foreground">
              {j.enabled && j.next_run ? timeAgo(j.next_run) : j.schedule ? 'Paused' : 'Manual'}
            </Td>
            <Td className="hidden sm:table-cell text-right tabular-nums">
              <div>{formatBytes(j.snapshots.latest_size)}</div>
              <div className="text-xs text-muted-foreground">{j.snapshots.count} snapshots</div>
            </Td>
            <Td>
              <Button
                variant="ghost"
                size="icon-sm"
                title="Run backup now"
                disabled={j.active_run_id !== 0}
                loading={runJob.isPending && runJob.variables === j.id}
                onClick={() => runJob.mutate(j.id)}
              >
                <Play />
              </Button>
            </Td>
          </Tr>
        ))}
      </tbody>
    </Table>
  )
}

export function RunsTable({ runs, showJob = true }: { runs: Run[]; showJob?: boolean }) {
  const navigate = useNavigate()
  return (
    <Table>
      <thead>
        <tr>
          <Th>Run</Th>
          {showJob && <Th className="hidden sm:table-cell">Job</Th>}
          <Th>Status</Th>
          <Th className="hidden md:table-cell text-right">Objects</Th>
          <Th className="hidden md:table-cell text-right">Data</Th>
          <Th className="hidden lg:table-cell text-right">Duration</Th>
        </tr>
      </thead>
      <tbody>
        {runs.map((r) => (
          <Tr key={r.id} className="cursor-pointer" onClick={() => navigate(`/runs/${r.id}`)}>
            <Td>
              <div className="font-medium">{kindLabel[r.kind] ?? r.kind}</div>
              <div className="text-xs text-muted-foreground" title={r.started_at}>
                #{r.id} · {timeAgo(r.started_at)}
              </div>
            </Td>
            {showJob && <Td className="hidden sm:table-cell">{r.job_name || '—'}</Td>}
            <Td>
              <StatusBadge status={r.status} />
            </Td>
            <Td className="hidden md:table-cell text-right tabular-nums">
              {formatNumber(r.objects_done)}
              {r.objects_failed > 0 && <span className="text-destructive"> ({r.objects_failed} failed)</span>}
            </Td>
            <Td className="hidden md:table-cell text-right tabular-nums">
              {formatBytes(r.bytes_done)}
              {r.kind === 'backup' && r.bytes_uploaded > 0 && (
                <div className="text-xs text-muted-foreground">{formatBytes(r.bytes_uploaded)} uploaded</div>
              )}
            </Td>
            <Td className="hidden lg:table-cell text-right tabular-nums text-muted-foreground">
              {formatDuration(r.started_at, r.finished_at)}
            </Td>
          </Tr>
        ))}
      </tbody>
    </Table>
  )
}
