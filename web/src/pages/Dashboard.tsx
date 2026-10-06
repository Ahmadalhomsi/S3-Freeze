import { Link } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { Activity, Archive, Check, Database, HardDrive, History, Plus, ShieldCheck } from 'lucide-react'
import { api, type Dashboard } from '@/lib/api'
import { formatBytes, formatNumber, timeAgo } from '@/lib/utils'
import { buttonVariants, Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui'
import { EmptyState, ErrorBox, kindLabel, Loading, PageHeader, RunProgress, Stat } from '@/components/common'
import { JobsTable, RunsTable } from '@/components/tables'
import { QuickBackupButton } from '@/components/QuickBackup'
import { cn } from '@/lib/utils'

export default function DashboardPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['dashboard'],
    queryFn: () => api.get<Dashboard>('/api/dashboard'),
    refetchInterval: (q) => (q.state.data?.active_runs.length ? 2000 : 15000),
  })

  if (isLoading) return <Loading />
  if (error || !data) return <ErrorBox error={error} />

  const h = data.health
  const total = data.jobs.length
  const problems = (h.failing ?? 0) + (h.stale ?? 0)

  return (
    <>
      <PageHeader
        title="Dashboard"
        description="Health of your object storage backups at a glance."
        actions={
          <>
            <Link to="/jobs/new" className={buttonVariants({ variant: 'outline' })}>
              <Plus /> New scheduled job
            </Link>
            <QuickBackupButton />
          </>
        }
      />

      {total === 0 ? (
        <Onboarding hasStorage={data.storages > 0} />
      ) : (
        <div className="flex flex-col gap-6">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Stat
              icon={ShieldCheck}
              label="Backup health"
              value={
                <span className={problems > 0 ? 'text-destructive' : 'text-success'}>
                  {(h.healthy ?? 0) + (h.warning ?? 0) + (h.running ?? 0)}/{total}
                </span>
              }
              sub={problems > 0 ? `${problems} job${problems > 1 ? 's' : ''} need attention` : 'All jobs are healthy'}
            />
            <Stat
              icon={Database}
              label="Protected data"
              value={formatBytes(data.protected_bytes)}
              sub={`${formatNumber(data.protected_objects)} objects in latest snapshots`}
            />
            <Stat icon={History} label="Snapshots" value={formatNumber(data.snapshots)} sub={`across ${total} job${total > 1 ? 's' : ''}`} />
            <Stat
              icon={Activity}
              label="Last 24 hours"
              value={`${data.runs_24h.success + data.runs_24h.warning} ok`}
              sub={
                data.runs_24h.failed > 0 ? (
                  <span className="text-destructive">{data.runs_24h.failed} failed runs</span>
                ) : (
                  'No failed runs'
                )
              }
            />
          </div>

          {data.active_runs.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>In progress</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-4">
                {data.active_runs.map((r) => (
                  <Link key={r.id} to={`/runs/${r.id}`} className="flex flex-col gap-2 rounded-lg border p-3 hover:bg-muted/50">
                    <div className="flex items-center justify-between text-sm">
                      <span className="font-medium">
                        {kindLabel[r.kind]} · {r.job_name}
                      </span>
                      <span className="text-xs text-muted-foreground">started {timeAgo(r.started_at)}</span>
                    </div>
                    <RunProgress run={r} />
                  </Link>
                ))}
              </CardContent>
            </Card>
          )}

          <Card>
            <CardHeader className="flex-row items-center justify-between">
              <div>
                <CardTitle>Backup jobs</CardTitle>
                <CardDescription className="mt-1">Status of every job based on its latest run and schedule.</CardDescription>
              </div>
              <Link to="/jobs" className={buttonVariants({ variant: 'outline', size: 'sm' })}>
                View all
              </Link>
            </CardHeader>
            <JobsTable jobs={data.jobs} />
          </Card>

          <Card>
            <CardHeader className="flex-row items-center justify-between">
              <CardTitle>Recent activity</CardTitle>
              <Link to="/runs" className={buttonVariants({ variant: 'outline', size: 'sm' })}>
                View all
              </Link>
            </CardHeader>
            {data.recent_runs.length ? (
              <RunsTable runs={data.recent_runs} />
            ) : (
              <EmptyState icon={Activity} title="No activity yet" />
            )}
          </Card>
        </div>
      )}
    </>
  )
}

function Onboarding({ hasStorage }: { hasStorage: boolean }) {
  const steps = [
    {
      done: hasStorage,
      icon: HardDrive,
      title: 'Connect your storages',
      text: 'Add the S3-compatible service you want to back up (MinIO, SeaweedFS, AWS…) and where backups should go — another S3 or a local disk.',
      to: '/storages',
      cta: 'Add storage',
    },
    {
      done: false,
      icon: Archive,
      title: 'Create a backup job',
      text: 'Pick a source bucket, a destination, a schedule and how many snapshots to keep. Optionally encrypt everything.',
      to: '/jobs/new',
      cta: 'Create job',
    },
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle>Get started</CardTitle>
        <CardDescription>Two steps until your first incremental, deduplicated snapshot.</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4 md:grid-cols-2">
        {steps.map((s, i) => (
          <div key={s.title} className="flex flex-col gap-3 rounded-lg border p-4">
            <div className="flex items-center gap-3">
              <div
                className={cn(
                  'flex size-8 items-center justify-center rounded-full text-sm font-semibold',
                  s.done ? 'bg-success/15 text-success' : 'bg-accent text-accent-foreground',
                )}
              >
                {s.done ? <Check className="size-4" /> : i + 1}
              </div>
              <p className="font-medium">{s.title}</p>
            </div>
            <p className="text-sm text-muted-foreground">{s.text}</p>
            <Link
              to={s.to}
              className={cn(buttonVariants({ variant: s.done ? 'outline' : 'default', size: 'sm' }), 'mt-auto self-start')}
            >
              <s.icon /> {s.cta}
            </Link>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}
