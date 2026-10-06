import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Activity } from 'lucide-react'
import { api, type Run } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Card } from '@/components/ui'
import { EmptyState, ErrorBox, Loading, PageHeader } from '@/components/common'
import { RunsTable } from '@/components/tables'

const filters = [
  { value: '', label: 'All' },
  { value: 'backup', label: 'Backups' },
  { value: 'restore', label: 'Restores' },
  { value: 'delete', label: 'Deletions' },
  { value: 'scan', label: 'Scans' },
]

export default function RunsPage() {
  const [kind, setKind] = useState('')
  const { data, isLoading, error } = useQuery({
    queryKey: ['runs', { kind }],
    queryFn: () => api.get<Run[]>(`/api/runs?limit=200${kind ? `&kind=${kind}` : ''}`),
    refetchInterval: (q) => (q.state.data?.some((r) => r.status === 'running') ? 2000 : 15000),
  })

  return (
    <>
      <PageHeader title="Activity" description="Every backup, restore and maintenance run with its full log." />
      <div className="mb-4 flex flex-wrap gap-1.5">
        {filters.map((f) => (
          <button
            key={f.value}
            onClick={() => setKind(f.value)}
            className={cn(
              'rounded-full border px-3 py-1 text-sm font-medium transition-colors',
              kind === f.value ? 'border-primary bg-accent text-accent-foreground' : 'bg-card hover:bg-muted',
            )}
          >
            {f.label}
          </button>
        ))}
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorBox error={error} />
      ) : (
        <Card>{data?.length ? <RunsTable runs={data} /> : <EmptyState icon={Activity} title="Nothing here yet" />}</Card>
      )}
    </>
  )
}
