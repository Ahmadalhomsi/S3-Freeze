import { useMemo, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { ArchiveRestore, ArrowLeft, Check, ChevronRight, Download, Eye, Folder, FolderOpen, LayoutGrid, List, Search, X } from 'lucide-react'
import { api, type Job, type Listing, type Snapshot } from '@/lib/api'
import { cn, formatBytes, formatDate, formatNumber } from '@/lib/utils'
import { Button, Card, Input, Table, Td, Th, Tr } from '@/components/ui'
import { EmptyState, ErrorBox, Loading, PageHeader } from '@/components/common'
import { RestoreDialog } from '@/components/RestoreDialog'
import { FileIcon, FilePreview, fileKind, fileUrl, type FileItem } from '@/components/FilePreview'

type View = 'list' | 'grid'

function savedView(): View {
  try {
    return localStorage.getItem('browser-view') === 'grid' ? 'grid' : 'list'
  } catch {
    return 'list'
  }
}

export default function SnapshotBrowserPage() {
  const { id = '', sid = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const path = params.get('path') ?? ''
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [restoring, setRestoring] = useState<string[] | null>(null)
  const [preview, setPreview] = useState<number | null>(null)
  const [filter, setFilter] = useState('')
  const [view, setViewState] = useState<View>(savedView)
  const setView = (v: View) => {
    setViewState(v)
    try {
      localStorage.setItem('browser-view', v)
    } catch {
      /* ignore */
    }
  }

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

  const q = filter.toLowerCase()
  const dirs = (listing.data?.dirs ?? []).filter((d) => d.name.toLowerCase().includes(q))
  const files = (listing.data?.files ?? []).filter((f) => f.name.toLowerCase().includes(q))

  const go = (p: string) => {
    setFilter('')
    setParams(p ? { path: p } : {})
  }
  const toggle = (p: string) =>
    setSelected((s) => {
      const n = new Set(s)
      if (n.has(p)) n.delete(p)
      else n.add(p)
      return n
    })

  const items = [...dirs.map((d) => d.path), ...files.map((f) => f.path)]
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
        description={
          snapshot
            ? `${job.data?.name ?? ''} · ${formatDate(snapshot.created_at)} · ${formatNumber(snapshot.objects)} objects, ${formatBytes(snapshot.size)}`
            : sid
        }
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
        <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
          <div className="flex min-w-0 flex-wrap items-center gap-1 text-sm">
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
          <div className="flex items-center gap-2">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter this folder" className="h-8 w-44 pl-8 text-xs" />
            </div>
            <div className="flex rounded-lg border p-0.5">
              {(
                [
                  ['list', List],
                  ['grid', LayoutGrid],
                ] as const
              ).map(([v, Icon]) => (
                <button
                  key={v}
                  title={v === 'list' ? 'List view' : 'Grid view'}
                  onClick={() => setView(v)}
                  className={cn('rounded-md p-1.5', view === v ? 'bg-muted text-foreground' : 'text-muted-foreground hover:text-foreground')}
                >
                  <Icon className="size-4" />
                </button>
              ))}
            </div>
          </div>
        </div>

        {listing.isLoading ? (
          <Loading />
        ) : listing.error ? (
          <div className="p-4">
            <ErrorBox error={listing.error} />
          </div>
        ) : !items.length ? (
          <EmptyState icon={Folder} title={filter ? 'No matches' : 'This folder is empty'} />
        ) : view === 'grid' ? (
          <div className="grid grid-cols-2 gap-3 p-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {dirs.map((d) => (
              <Tile key={d.path} selected={selected.has(d.path)} onToggle={() => toggle(d.path)} onOpen={() => go(d.path)} title={d.name} sub={`${formatNumber(d.objects)} items`}>
                <Folder className="size-10 fill-primary/15 text-primary" />
              </Tile>
            ))}
            {files.map((f, i) => (
              <Tile key={f.path} selected={selected.has(f.path)} onToggle={() => toggle(f.path)} onOpen={() => setPreview(i)} title={f.name} sub={formatBytes(f.size)}>
                {fileKind(f) === 'image' ? (
                  <img src={fileUrl(id, sid, f.path)} alt="" loading="lazy" decoding="async" className="size-full object-cover" />
                ) : (
                  <FileIcon file={f} className="size-10" />
                )}
              </Tile>
            ))}
          </div>
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
                <Th className="w-24" />
              </tr>
            </thead>
            <tbody>
              {dirs.map((d) => (
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
                  <Td />
                </Tr>
              ))}
              {files.map((f, i) => (
                <Tr key={f.path}>
                  <Td>
                    <input type="checkbox" className="accent-[var(--primary)]" checked={selected.has(f.path)} onChange={() => toggle(f.path)} />
                  </Td>
                  <Td>
                    <button onClick={() => setPreview(i)} className="flex items-center gap-2 text-left hover:text-primary">
                      <FileIcon file={f} />
                      <span className="break-all">{f.name}</span>
                    </button>
                  </Td>
                  <Td className="text-right tabular-nums">{formatBytes(f.size)}</Td>
                  <Td className="hidden md:table-cell text-muted-foreground">{formatDate(f.modified)}</Td>
                  <Td>
                    <div className="flex justify-end gap-0.5">
                      <Button variant="ghost" size="icon-sm" title="Preview" onClick={() => setPreview(i)}>
                        <Eye />
                      </Button>
                      <a href={fileUrl(id, sid, f.path, 'download')} title="Download" className="inline-flex size-8 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground">
                        <Download className="size-4" />
                      </a>
                    </div>
                  </Td>
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

      {preview !== null && files[preview] && (
        <FilePreview
          jobId={id}
          snapId={sid}
          files={files as FileItem[]}
          index={preview}
          onIndex={setPreview}
          onClose={() => setPreview(null)}
          onRestore={(p) => {
            setPreview(null)
            setRestoring([p])
          }}
        />
      )}

      {restoring && job.data && snapshot && (
        <RestoreDialog job={job.data} snapshot={snapshot} paths={restoring} onClose={() => setRestoring(null)} />
      )}
    </>
  )
}

function Tile({
  children,
  title,
  sub,
  selected,
  onToggle,
  onOpen,
}: {
  children: React.ReactNode
  title: string
  sub: string
  selected: boolean
  onToggle: () => void
  onOpen: () => void
}) {
  return (
    <div
      className={cn(
        'group relative flex flex-col overflow-hidden rounded-xl border bg-card transition-shadow hover:shadow-md',
        selected && 'ring-2 ring-primary',
      )}
    >
      <button onClick={onOpen} className="flex aspect-square items-center justify-center overflow-hidden bg-muted/50">
        {children}
      </button>
      <button
        onClick={onToggle}
        title={selected ? 'Deselect' : 'Select'}
        className={cn(
          'absolute left-2 top-2 flex size-5 items-center justify-center rounded-md border bg-card/90 shadow-sm backdrop-blur transition-opacity',
          selected ? 'border-primary bg-primary text-primary-foreground opacity-100' : 'opacity-0 group-hover:opacity-100',
        )}
      >
        {selected && <Check className="size-3.5" />}
      </button>
      <button onClick={onOpen} className="flex flex-col items-start px-2.5 py-2 text-left">
        <span className="w-full truncate text-xs font-medium" title={title}>
          {title}
        </span>
        <span className="text-[11px] text-muted-foreground">{sub}</span>
      </button>
    </div>
  )
}
