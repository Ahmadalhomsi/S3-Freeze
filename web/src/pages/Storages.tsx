import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import { BarChart3, CheckCircle2, Cloud, Folder, HardDrive, Pencil, Plus, Trash2, XCircle, Zap } from 'lucide-react'
import { toast } from 'sonner'
import { api, type Storage, type StorageInput } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useQuickBackup } from '@/components/QuickBackup'
import { DiskSpace } from '@/components/LocationPicker'
import {
  Badge,
  Button,
  buttonVariants,
  Card,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Field,
  Input,
  Label,
  Select,
  SwitchField,
} from '@/components/ui'
import { ConfirmDialog, EmptyState, ErrorBox, Loading, PageHeader } from '@/components/common'

type Proto = 'https' | 'http'

interface Preset {
  id: string
  label: string
  proto: Proto
  host: string
  port: string
  placeholder: string
  region: string
  pathStyle: boolean
  hint: string
}

const presets: Preset[] = [
  {
    id: 'seaweed', label: 'SeaweedFS', proto: 'https', host: '', port: '', placeholder: 's3.example.com', region: 'us-east-1', pathStyle: true,
    hint: 'Your SeaweedFS S3 gateway. Inside the same Docker network use HTTP, host seaweedfs and port 8333.',
  },
  {
    id: 'minio', label: 'MinIO', proto: 'https', host: '', port: '', placeholder: 'minio.example.com', region: 'us-east-1', pathStyle: true,
    hint: 'Your MinIO API. Inside the same Docker network use HTTP, host minio and port 9000.',
  },
  {
    id: 'aws', label: 'AWS S3', proto: 'https', host: 's3.us-east-1.amazonaws.com', port: '', placeholder: 's3.us-east-1.amazonaws.com', region: 'us-east-1', pathStyle: false,
    hint: 'Use the endpoint of your bucket region, e.g. s3.eu-central-1.amazonaws.com.',
  },
  {
    id: 'r2', label: 'Cloudflare R2', proto: 'https', host: '', port: '', placeholder: '<account-id>.r2.cloudflarestorage.com', region: 'auto', pathStyle: true,
    hint: 'Found in Cloudflare → R2 → Manage API tokens.',
  },
  {
    id: 'other', label: 'Other', proto: 'https', host: '', port: '', placeholder: 's3.example.com', region: 'us-east-1', pathStyle: true,
    hint: 'Any S3-compatible endpoint: Backblaze B2, Wasabi, Garage, Ceph…',
  },
]

const defaultPort = (p: Proto) => (p === 'https' ? '443' : '80')

/** Splits "https://host:9000/x" (or a bare host) into protocol, host and port. */
function splitEndpoint(endpoint: string, fallback: Proto): { proto: Proto; host: string; port: string } {
  let s = endpoint.trim()
  let proto = fallback
  const scheme = s.match(/^(https?):\/\//i)
  if (scheme) {
    proto = scheme[1].toLowerCase() as Proto
    s = s.slice(scheme[0].length)
  }
  s = s.replace(/\/.*$/, '')
  let host = s
  let port = ''
  const m = s.match(/^(.*):(\d+)$/)
  if (m) {
    host = m[1]
    port = m[2]
  }
  if (port === defaultPort(proto)) port = ''
  return { proto, host, port }
}

function joinEndpoint(proto: Proto, host: string, port: string): string {
  const h = host.trim()
  const p = port.trim()
  if (!h) return ''
  return `${proto}://${h}${p && p !== defaultPort(proto) ? `:${p}` : ''}`
}

const emptyInput: StorageInput = {
  name: '',
  type: 's3',
  endpoint: '',
  region: 'us-east-1',
  access_key: '',
  secret_key: '',
  use_ssl: true,
  path_style: true,
  local_path: '/backups',
}

export default function StoragesPage() {
  const qc = useQueryClient()
  const { data, isLoading, error } = useQuery({ queryKey: ['storages'], queryFn: () => api.get<Storage[]>('/api/storages') })
  const [editing, setEditing] = useState<Storage | null | 'new'>(null)
  const [deleting, setDeleting] = useState<Storage | null>(null)
  const quick = useQuickBackup()

  const addButton = (
    <Button onClick={() => setEditing('new')}>
      <Plus /> Add storage
    </Button>
  )

  return (
    <>
      <PageHeader
        title="Storages"
        description="Connections to S3-compatible services and local disks, used as backup sources and destinations."
        actions={addButton}
      />
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorBox error={error} />
      ) : !data?.length ? (
        <Card>
          <EmptyState
            icon={HardDrive}
            title="No storages yet"
            description="Add the S3 service you want to back up, and a destination for the backups."
            action={addButton}
          />
        </Card>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {data.map((s) => (
            <Card key={s.id} className="flex flex-col gap-3 p-5">
              <div className="flex items-start justify-between gap-2">
                <div className="flex min-w-0 items-center gap-3">
                  <div className="rounded-lg bg-accent p-2 text-accent-foreground">
                    {s.type === 's3' ? <Cloud className="size-4" /> : <Folder className="size-4" />}
                  </div>
                  <div className="min-w-0">
                    <Link to={`/storages/${s.id}`} className="block truncate font-medium hover:text-primary">
                      {s.name}
                    </Link>
                    <Badge variant={s.builtin ? 'primary' : 'outline'} className="mt-0.5">
                      {s.builtin ? 'Built-in · this server' : s.type === 's3' ? 'S3-compatible' : 'Local disk'}
                    </Badge>
                  </div>
                </div>
                <div className="flex shrink-0">
                  <Button variant="ghost" size="icon-sm" title="Edit" onClick={() => setEditing(s)}>
                    <Pencil />
                  </Button>
                  {!s.builtin && (
                    <Button variant="ghost" size="icon-sm" title="Delete" onClick={() => setDeleting(s)}>
                      <Trash2 />
                    </Button>
                  )}
                </div>
              </div>
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
                {s.type === 's3' ? (
                  <>
                    <dt className="text-muted-foreground">Endpoint</dt>
                    <dd className="truncate font-mono">{s.endpoint}</dd>
                    <dt className="text-muted-foreground">Access key</dt>
                    <dd className="truncate font-mono">{s.access_key || '—'}</dd>
                    <dt className="text-muted-foreground">Region</dt>
                    <dd className="truncate">{s.region || '—'}</dd>
                  </>
                ) : (
                  <>
                    <dt className="text-muted-foreground">Path</dt>
                    <dd className="truncate font-mono">{s.local_path}</dd>
                  </>
                )}
              </dl>
              {s.type === 'local' && <DiskSpace storageId={s.id} />}
              {s.builtin && (
                <p className="text-xs text-muted-foreground">
                  Always available as a backup destination. Keep this path on a persistent Docker volume.
                </p>
              )}
              <div className="mt-auto flex gap-2 border-t pt-3">
                <Link to={`/storages/${s.id}`} className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), 'flex-1')}>
                  <BarChart3 /> {s.type === 's3' ? 'Buckets & sizes' : 'Folders & sizes'}
                </Link>
                {!s.builtin && (
                  <Button size="sm" className="flex-1" onClick={() => quick({ storage_id: s.id, bucket: '', prefix: '' })}>
                    <Zap /> Back up
                  </Button>
                )}
              </div>
            </Card>
          ))}
        </div>
      )}

      {editing && (
        <StorageDialog
          storage={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => qc.invalidateQueries({ queryKey: ['storages'] })}
        />
      )}

      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(v) => !v && setDeleting(null)}
        title={`Delete storage “${deleting?.name}”?`}
        description="The connection will be removed. Data stored on it is not touched. Storages used by a job cannot be deleted."
        confirmLabel="Delete"
        destructive
        onConfirm={async () => {
          try {
            await api.del(`/api/storages/${deleting!.id}`)
            toast.success('Storage deleted')
            qc.invalidateQueries({ queryKey: ['storages'] })
          } catch (err) {
            toast.error((err as Error).message)
          }
        }}
      />
    </>
  )
}

export function StorageDialog({ storage, onClose, onSaved }: { storage: Storage | null; onClose: () => void; onSaved: () => void }) {
  const [form, setForm] = useState<StorageInput>(() =>
    storage
      ? {
          name: storage.name,
          type: storage.type,
          endpoint: storage.endpoint,
          region: storage.region,
          access_key: storage.access_key,
          secret_key: '',
          use_ssl: storage.use_ssl,
          path_style: storage.path_style,
          local_path: storage.local_path || emptyInput.local_path,
        }
      : { ...emptyInput, name: presets[0].label, region: presets[0].region, path_style: presets[0].pathStyle },
  )
  // The endpoint is edited as protocol + host + port and joined on save.
  const [conn, setConn] = useState(() =>
    storage
      ? splitEndpoint(storage.endpoint, storage.use_ssl ? 'https' : 'http')
      : { proto: presets[0].proto, host: presets[0].host, port: presets[0].port },
  )
  const [preset, setPreset] = useState(storage ? 'other' : presets[0].id)
  const [test, setTest] = useState<{ ok: boolean; error?: string; buckets?: number } | null>(null)
  const set = <K extends keyof StorageInput>(k: K, v: StorageInput[K]) => {
    setForm((f) => ({ ...f, [k]: v }))
    setTest(null)
  }
  const setConnField = (patch: Partial<typeof conn>) => {
    setConn((c) => ({ ...c, ...patch }))
    setTest(null)
  }

  const endpoint = joinEndpoint(conn.proto, conn.host, conn.port)
  const payload = (): StorageInput => (form.type === 's3' ? { ...form, endpoint, use_ssl: conn.proto === 'https' } : form)

  const save = useMutation({
    mutationFn: () => (storage ? api.put(`/api/storages/${storage.id}`, payload()) : api.post('/api/storages', payload())),
    onSuccess: () => {
      toast.success(storage ? 'Storage updated' : 'Storage added')
      onSaved()
      onClose()
    },
  })
  const testConn = useMutation({
    mutationFn: () => api.post<{ ok: boolean; error?: string; buckets?: number }>('/api/storages/test', { ...payload(), id: storage?.id ?? 0 }),
    onSuccess: setTest,
    onError: (e) => setTest({ ok: false, error: e.message }),
  })

  const applyPreset = (id: string) => {
    const p = presets.find((x) => x.id === id)!
    setPreset(id)
    setForm((f) => ({
      ...f,
      name: !f.name || presets.some((x) => x.label === f.name) ? p.label : f.name,
      region: p.region,
      path_style: p.pathStyle,
    }))
    setConn({ proto: p.proto, host: p.host, port: p.port })
    setTest(null)
  }
  const currentPreset = presets.find((p) => p.id === preset) ?? presets[presets.length - 1]

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{storage ? 'Edit storage' : 'Add storage'}</DialogTitle>
          <DialogDescription>Credentials are encrypted at rest with the server's master key.</DialogDescription>
        </DialogHeader>

        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          {storage?.builtin && (
            <div className="rounded-lg bg-muted/60 p-3 text-sm">
              Built-in storage at <code className="font-mono text-xs">{storage.local_path}</code>. The path comes from the{' '}
              <code className="font-mono text-xs">BACKUP_DIR</code> setting (<code className="font-mono text-xs">/backups</code> in
              Docker), so only the name can be changed here.
            </div>
          )}
          <div className={cn('grid grid-cols-2 gap-2 rounded-lg bg-muted p-1', storage?.builtin && 'hidden')}>
            {(['s3', 'local'] as const).map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => set('type', t)}
                className={cn(
                  'flex items-center justify-center gap-2 rounded-md py-1.5 text-sm font-medium transition-colors',
                  form.type === t ? 'bg-card shadow-sm' : 'text-muted-foreground hover:text-foreground',
                )}
              >
                {t === 's3' ? <Cloud className="size-4" /> : <Folder className="size-4" />}
                {t === 's3' ? 'S3-compatible' : 'Local disk'}
              </button>
            ))}
          </div>

          {form.type === 's3' && !storage && (
            <div className="flex flex-wrap gap-1.5">
              {presets.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => applyPreset(p.id)}
                  className={cn(
                    'rounded-full border px-3 py-1 text-xs font-medium transition-colors',
                    preset === p.id ? 'border-primary bg-accent text-accent-foreground' : 'hover:bg-muted',
                  )}
                >
                  {p.label}
                </button>
              ))}
            </div>
          )}

          <Field label="Name">
            <Input value={form.name} onChange={(e) => set('name', e.target.value)} placeholder="Production SeaweedFS" required />
          </Field>

          {form.type === 's3' ? (
            <>
              <div className="flex flex-col gap-1.5">
                <Label>Endpoint</Label>
                <div className="grid grid-cols-[7.5rem_1fr_6rem] gap-2">
                  <Select
                    aria-label="Protocol"
                    value={conn.proto}
                    onChange={(e) => {
                      const proto = e.target.value as Proto
                      // A port that was the old protocol's default follows the new one.
                      setConnField({ proto, port: conn.port === defaultPort(conn.proto) ? '' : conn.port })
                    }}
                  >
                    <option value="https">HTTPS</option>
                    <option value="http">HTTP</option>
                  </Select>
                  <Input
                    aria-label="Host"
                    value={conn.host}
                    placeholder={currentPreset.placeholder}
                    required
                    autoComplete="off"
                    spellCheck={false}
                    onChange={(e) => {
                      const v = e.target.value
                      // Pasting a full URL fills protocol, host and port at once.
                      if (/^https?:\/\//i.test(v) || /:\d+$/.test(v)) setConnField(splitEndpoint(v, conn.proto))
                      else setConnField({ host: v })
                    }}
                  />
                  <Input
                    aria-label="Port"
                    value={conn.port}
                    placeholder={defaultPort(conn.proto)}
                    inputMode="numeric"
                    onChange={(e) => setConnField({ port: e.target.value.replace(/\D/g, '').slice(0, 5) })}
                  />
                </div>
                <p className="text-xs text-muted-foreground">
                  {currentPreset.hint} Leave the port empty to use the default ({defaultPort(conn.proto)}).
                </p>
                {endpoint && (
                  <p className="text-xs text-muted-foreground">
                    Connects to <code className="font-mono text-foreground">{endpoint}</code>
                  </p>
                )}
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Access key">
                  <Input value={form.access_key} onChange={(e) => set('access_key', e.target.value)} autoComplete="off" />
                </Field>
                <Field label="Secret key">
                  <Input
                    type="password"
                    value={form.secret_key}
                    onChange={(e) => set('secret_key', e.target.value)}
                    placeholder={storage?.has_secret ? '•••••••• (unchanged)' : ''}
                    autoComplete="new-password"
                  />
                </Field>
              </div>
              <Field label="Region" hint="Most self-hosted services accept us-east-1.">
                <Input value={form.region} onChange={(e) => set('region', e.target.value)} />
              </Field>
              <div className="rounded-lg border p-4">
                <SwitchField
                  label="Path-style addressing"
                  description="Required by SeaweedFS, MinIO and most self-hosted services."
                  checked={form.path_style}
                  onCheckedChange={(v) => set('path_style', v)}
                />
              </div>
            </>
          ) : storage?.builtin ? null : (
            <Field
              label="Directory"
              hint="A path inside the container. Mount a volume there (e.g. /backups) so data survives redeploys. Subfolders act as buckets."
            >
              <Input value={form.local_path} onChange={(e) => set('local_path', e.target.value)} placeholder="/backups" required />
            </Field>
          )}

          {test && (
            <div
              className={cn(
                'flex items-start gap-2 rounded-lg border px-3 py-2 text-sm',
                test.ok ? 'border-success/30 bg-success/10 text-success' : 'border-destructive/30 bg-destructive/10 text-destructive',
              )}
            >
              {test.ok ? <CheckCircle2 className="mt-0.5 size-4 shrink-0" /> : <XCircle className="mt-0.5 size-4 shrink-0" />}
              <span className="break-all">
                {test.ok ? `Connection successful${test.buckets !== undefined ? ` · ${test.buckets} buckets visible` : ''}` : test.error}
              </span>
            </div>
          )}
          <ErrorBox error={save.error} />

          <DialogFooter className="sm:justify-between">
            <Button type="button" variant="outline" loading={testConn.isPending} onClick={() => testConn.mutate()}>
              Test connection
            </Button>
            <div className="flex flex-col-reverse gap-2 sm:flex-row">
              <Button type="button" variant="ghost" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" loading={save.isPending}>
                {storage ? 'Save changes' : 'Add storage'}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
