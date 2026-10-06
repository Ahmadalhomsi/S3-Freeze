import { useMemo, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { ArchiveRestore, ArrowLeft, ChevronRight, File, Folder, FolderOpen, X } from 'lucide-react'
import { api, type Job, type Listing, type Snapshot } from '@/lib/api'
import { formatBytes, formatDate, formatNumber } from '@/lib/utils'
import { Button, Card, Table, Td, Th, Tr } from '@/components/ui'
import { EmptyState, ErrorBox, Loading, PageHeader } from '@/components/common'
import { RestoreDialog } from '@/components/RestoreDialog'

export default function SnapshotBrowserPage() {
  const { id, sid } = useParams()
  const [params, setParams] = useSearchParams()
  const path = params.get('path') ?? ''
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [restoring, setRestoring] = useState<string[] | null>(null)

  const job = useQuery({ queryKey: ['job', id], queryFn: () => api.get<Job>(`/api/jobs/${id}`) })
  const snaps = useQuery({ queryKey: ['snapshots', id], queryFn: () => api.get<Snapshot[]>(`/api/jobs/${id}/snapshots`) })
  const listing = useQuery({
    queryKey: ['browse', id, sid, path],
    queryFn: () => api.get<Listing>(`/api/jobs/${id}/snapshots/${sid}/browse?path=${encodeURIComponent(path)}`),
    staleTime: Infinity,
  })
  const snapshot = snaps.data?.find((s) => s.id === sid)

  const crumbs = useMemo(() => {
    const parts = path.split('/').filter(Boolean)
    return parts.map((name, i) => ({ name, path: parts.slice(0, i + 1).join('/') + '/' }))
  }, [path])

  const go = (p: string) => setParams(p ? { path: p } : {})
  const toggle = (p: string) =>
    setSelected((s) => {
      const n = new Set(s)
      if (n.has(p)) n.delete(p)
      else n.add(p)
      return n
    })

  const items = listing.data ? [...listing.data.dirs.map((d) => d.path), ...listing.data.files.map((f) => f.path)] : []
  const allSelected = items.length > 0 && items.every((p) => selected.has(p))
  const toggleAll = () =>
    setSelected((s) => {
      const n = new Set(s)
      if (allSelected) items.forEach((p) => n.delete(p))
      else items.forEach((p) => n.add(p))
      return n
    })

  return (
    <>
      <PageHeader
        title="Browse snapshot"
        description={snapshot ? `${job.data?.name ?? ''} · ${formatDate(snapshot.created_at)} · ${formatNumber(snapshot.objects)} objects, ${formatBytes(snapshot.size)}` : sid}
        actions={
          <Button variant="outline" onClick={() => setRestoring([])} disabled={!job.data || !snapshot}>
            <ArchiveRestore /> Restore entire snapshot
          </Button>
        }
      >
        <Link to={`/jobs/${id}`} className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" /> {job.data?.name ?? 'Job'}
        </Link>
      </PageHeader>

      <Card className="overflow-hidden">
        <div className="flex flex-wrap items-center gap-1 border-b px-4 py-3 text-sm">
          <button onClick={() => go('')} className="flex items-center gap-1.5 rounded px-1.5 py-0.5 font-medium hover:bg-muted">
            <FolderOpen className="size-4 text-primary" /> {job.data?.source_bucket || 'root'}
          </button>
          {crumbs.map((c) => (
            <span key={c.path} className="flex items-center gap-1">
              <ChevronRight className="size-4 text-muted-foreground" />
              <button onClick={() => go(c.path)} className="rounded px-1.5 py-0.5 hover:bg-muted">
                {c.name}
              </button>
            </span>
          ))}
        </div>

        {listing.isLoading ? (
          <Loading />
        ) : listing.error ? (
          <div className="p-4">
            <ErrorBox error={listing.error} />
          </div>
        ) : !items.length ? (
          <EmptyState icon={Folder} title="This folder is empty" />
        ) : (
          <Table>
            <thead>
              <tr>
                <Th className="w-10">
                  <input type="checkbox" className="accent-[var(--primary)]" checked={allSelected} onChange={toggleAll} />
                </Th>
                <Th>Name</Th>
                <Th className="text-right">Size</Th>
                <Th className="hidden md:table-cell">Modified</Th>
              </tr>
            </thead>
            <tbody>
              {listing.data!.dirs.map((d) => (
                <Tr key={d.path}>
                  <Td>
                    <input type="checkbox" className="accent-[var(--primary)]" checked={selected.has(d.path)} onChange={() => toggle(d.path)} />
                  </Td>
                  <Td>
                    <button onClick={() => go(d.path)} className="flex items-center gap-2 font-medium hover:text-primary">
                      <Folder className="size-4 fill-primary/15 text-primary" />
                      {d.name}
                    </button>
                  </Td>
                  <Td className="text-right tabular-nums">
                    {formatBytes(d.size)}
                    <div className="text-xs text-muted-foreground">{formatNumber(d.objects)} objects</div>
                  </Td>
                  <Td className="hidden md:table-cell text-muted-foreground">—</Td>
                </Tr>
              ))}
              {listing.data!.files.map((f) => (
                <Tr key={f.path}>
                  <Td>
                    <input type="checkbox" className="accent-[var(--primary)]" checked={selected.has(f.path)} onChange={() => toggle(f.path)} />
                  </Td>
                  <Td>
                    <span className="flex items-center gap-2">
                      <File className="size-4 text-muted-foreground" />
                      <span className="break-all">{f.name}</span>
                    </span>
                  </Td>
                  <Td className="text-right tabular-nums">{formatBytes(f.size)}</Td>
                  <Td className="hidden md:table-cell text-muted-foreground">{formatDate(f.modified)}</Td>
                </Tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {selected.size > 0 && (
        <div className="fixed inset-x-0 bottom-4 z-30 flex justify-center px-4 lg:pl-60">
          <div className="flex items-center gap-3 rounded-xl border bg-card px-4 py-2.5 shadow-lg">
            <span className="text-sm font-medium">{selected.size} selected</span>
            <Button variant="ghost" size="sm" onClick={() => setSelected(new Set())}>
              <X /> Clear
            </Button>
            <Button size="sm" onClick={() => setRestoring([...selected])}>
              <ArchiveRestore /> Restore selected
            </Button>
          </div>
        </div>
      )}

      {restoring && job.data && snapshot && (
        <RestoreDialog job={job.data} snapshot={snapshot} paths={restoring} onClose={() => setRestoring(null)} />
      )}
    </>
  )
}
