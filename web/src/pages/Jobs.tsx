import { Link } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { Archive, Plus } from 'lucide-react'
import { api, type Job } from '@/lib/api'
import { buttonVariants, Card } from '@/components/ui'
import { EmptyState, ErrorBox, Loading, PageHeader } from '@/components/common'
import { JobsTable } from '@/components/tables'

export default function JobsPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['jobs'],
    queryFn: () => api.get<Job[]>('/api/jobs'),
    refetchInterval: (q) => (q.state.data?.some((j) => j.active_run_id) ? 2000 : 15000),
  })

  const newJob = (
    <Link to="/jobs/new" className={buttonVariants()}>
      <Plus /> New backup job
    </Link>
  )

  return (
    <>
      <PageHeader title="Backup jobs" description="Each job snapshots a bucket (or prefix) into a repository on schedule." actions={newJob} />
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorBox error={error} />
      ) : (
        <Card>
          {data?.length ? (
            <JobsTable jobs={data} />
          ) : (
            <EmptyState icon={Archive} title="No backup jobs yet" description="Create a job to start protecting a bucket." action={newJob} />
          )}
        </Card>
      )}
    </>
  )
}
