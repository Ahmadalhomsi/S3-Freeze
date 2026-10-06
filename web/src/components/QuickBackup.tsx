import { createContext, useContext, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, Cloud, HardDrive, KeyRound, Server, Zap } from 'lucide-react'
import { toast } from 'sonner'
import { api, type QuickBackupInput, type QuickBackupResult, type Storage } from '@/lib/api'
import { useStorages } from '@/lib/hooks'
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Field,
  Input,
  SwitchField,
} from '@/components/ui'
import { ErrorBox, Loading } from '@/components/common'
import { DiskSpace, LocationPicker, type Location } from '@/components/LocationPicker'
import { cn, defaultFolder } from '@/lib/utils'

type Open = (initial?: Partial<Location>) => void

const QuickBackupContext = createContext<Open>(() => {})

/** Opens the "Back up now" dialog from anywhere in the app. */
export function useQuickBackup() {
  return useContext(QuickBackupContext)
}

export function QuickBackupProvider({ children }: { children: React.ReactNode }) {
  const [initial, setInitial] = useState<Partial<Location> | null>(null)
  return (
    <QuickBackupContext.Provider value={(i) => setInitial(i ?? {})}>
      {children}
      {initial && <QuickBackupDialog initial={initial} onClose={() => setInitial(null)} />}
    </QuickBackupContext.Provider>
  )
}

export function QuickBackupButton({ size, className }: { size?: 'sm' | 'default' | 'lg'; className?: string }) {
  const open = useQuickBackup()
  return (
    <Button size={size} className={className} onClick={() => open()}>
      <Zap /> Back up now
    </Button>
  )
}

function QuickBackupDialog({ initial, onClose }: { initial: Partial<Location>; onClose: () => void }) {
  const { data: storages, isLoading } = useStorages()
  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Zap className="size-5 text-primary" /> Back up now
          </DialogTitle>
          <DialogDescription>Take an incremental snapshot right away. No schedule needed.</DialogDescription>
        </DialogHeader>
        {isLoading ? (
          <Loading />
        ) : !storages?.some((s) => !s.builtin) ? (
          <div className="flex flex-col items-start gap-3 text-sm">
            <p>Add the S3 storage you want to back up first (MinIO, SeaweedFS, AWS…). Backups can then go to this server’s disk.</p>
            <Link to="/storages" onClick={onClose} className="font-medium text-primary hover:underline">
              <HardDrive className="mr-1 inline size-4" /> Go to storages
            </Link>
          </div>
        ) : (
          <QuickBackupForm storages={storages} initial={initial} onClose={onClose} />
        )}
      </DialogContent>
    </Dialog>
  )
}

function QuickBackupForm({ storages, initial, onClose }: { storages: Storage[]; initial: Partial<Location>; onClose: () => void }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const builtin = storages.find((s) => s.builtin)
  const sources = storages.filter((s) => !s.builtin)
  // Default to the first S3 storage that was added: usually the one to protect.
  const byAge = [...sources].sort((a, b) => a.id - b.id)
  const firstSource = byAge.find((s) => s.type === 's3') ?? byAge[0] ?? storages[0]
  const [src, setSrc] = useState<Location>({
    storage_id: initial.storage_id ?? firstSource.id,
    bucket: initial.bucket ?? '',
    prefix: initial.prefix ?? '',
  })
  // Default destination: the server's own disk (a Docker volume).
  const [target, setTarget] = useState<'server' | 'other'>(builtin ? 'server' : 'other')
  // The folder is named after the source (e.g. "minio-photos") until the user edits it.
  const folderFor = (l: Location) => defaultFolder(storages.find((s) => s.id === l.storage_id)?.name, l.bucket)
  const [folderTouched, setFolderTouched] = useState(false)
  const [serverFolder, setServerFolder] = useState(() => folderFor(src))
  const [other, setOther] = useState<Location>(() => {
    const o = storages.find((s) => s.id !== src.storage_id && !s.builtin) ?? storages.find((s) => s.id !== src.storage_id)
    return { storage_id: (o ?? storages[0]).id, bucket: '', prefix: folderFor(src) }
  })
  useEffect(() => {
    if (folderTouched) return
    const f = folderFor(src)
    setServerFolder(f)
    setOther((o) => ({ ...o, prefix: f }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [src.storage_id, src.bucket, folderTouched])

  const dest: Location =
    target === 'server' && builtin ? { storage_id: builtin.id, bucket: '', prefix: serverFolder } : other
  const [encryption, setEncryption] = useState(false)
  const [passphrase, setPassphrase] = useState('')

  const start = useMutation({
    mutationFn: () => {
      const body: QuickBackupInput = {
        source_storage_id: src.storage_id,
        source_bucket: src.bucket,
        source_prefix: src.prefix,
        dest_storage_id: dest.storage_id,
        dest_bucket: dest.bucket,
        dest_prefix: dest.prefix,
        encryption,
        passphrase,
      }
      return api.post<QuickBackupResult>('/api/backups', body)
    },
    onSuccess: (r) => {
      qc.invalidateQueries()
      toast.success(r.existing_job ? `Backup started (job “${r.job_name}”)` : `Backup started — saved as job “${r.job_name}”`)
      onClose()
      navigate(`/runs/${r.run_id}`)
    },
  })

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        start.mutate()
      }}
    >
      <section className="flex flex-col gap-3 rounded-xl border p-4">
        <p className="text-sm font-semibold">What to back up</p>
        <LocationPicker storages={sources.length ? sources : storages} value={src} onChange={setSrc} scope showSize />
      </section>

      <div className="-my-2 flex justify-center text-muted-foreground">
        <ArrowDown className="size-5" />
      </div>

      <section className="flex flex-col gap-3 rounded-xl border p-4">
        <p className="text-sm font-semibold">Where to store it</p>
        {builtin && (
          <div className="grid gap-2 sm:grid-cols-2">
            {(
              [
                ['server', Server, 'This server', 'Docker volume, no setup needed'],
                ['other', Cloud, 'Another storage', 'S3 bucket or other disk'],
              ] as const
            ).map(([value, Icon, title, desc]) => (
              <button
                key={value}
                type="button"
                onClick={() => setTarget(value)}
                className={cn(
                  'flex items-center gap-3 rounded-lg border p-3 text-left transition-colors',
                  target === value ? 'border-primary bg-accent/60 ring-1 ring-primary' : 'hover:bg-muted',
                )}
              >
                <Icon className={cn('size-5 shrink-0', target === value ? 'text-primary' : 'text-muted-foreground')} />
                <span className="min-w-0">
                  <span className="block text-sm font-medium">{title}</span>
                  <span className="block truncate text-xs text-muted-foreground">{desc}</span>
                </span>
              </button>
            ))}
          </div>
        )}

        {target === 'server' && builtin ? (
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-2 rounded-lg bg-muted/60 p-3">
              <p className="flex items-center gap-2 text-sm">
                <HardDrive className="size-4 text-muted-foreground" />
                <code className="font-mono text-xs">{builtin.local_path}</code>
              </p>
              <DiskSpace storageId={builtin.id} />
            </div>
            <Field label="Folder" hint="Snapshots go into this folder on the server disk. Reusing it deduplicates across backups.">
              <Input
                value={serverFolder}
                onChange={(e) => {
                  setFolderTouched(true)
                  setServerFolder(e.target.value)
                }}
                placeholder={folderFor(src)}
                required
              />
            </Field>
            <p className="text-xs text-muted-foreground">
              Kept across redeploys as long as this path is a persistent volume (it is with the provided docker-compose; on
              Coolify add a persistent storage mounted at <code className="font-mono">/backups</code>).
            </p>
          </div>
        ) : (
          <>
            <LocationPicker
              storages={storages}
              value={other}
              onChange={(v) => {
                // Switching storage or bucket keeps the folder name.
                if (v.storage_id !== other.storage_id || v.bucket !== other.bucket) return setOther({ ...v, prefix: other.prefix })
                if (v.prefix !== other.prefix) setFolderTouched(true)
                setOther(v)
              }}
              folderHint="Snapshots are stored in this folder (a repository). Reusing it deduplicates across backups."
            />
            {other.storage_id === src.storage_id && (
              <p className="text-xs text-muted-foreground">
                Same storage as the source: the backup folder is excluded automatically so backups never include themselves.
              </p>
            )}
          </>
        )}
      </section>

      <section className="flex flex-col gap-3 rounded-xl border p-4">
        <SwitchField
          label="Encrypt"
          description="AES-256 on this server before upload. Only applies when this destination folder is new."
          checked={encryption}
          onCheckedChange={setEncryption}
        />
        {encryption && (
          <Field label="Passphrase" hint="At least 8 characters. Keep it safe — it is required to restore.">
            <div className="relative">
              <KeyRound className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                type="password"
                className="pl-9"
                value={passphrase}
                onChange={(e) => setPassphrase(e.target.value)}
                minLength={8}
                required
                autoComplete="new-password"
              />
            </div>
          </Field>
        )}
      </section>

      <p className="text-xs text-muted-foreground">
        The snapshot is saved under a manual job (no schedule) so you can browse, restore or re-run it later. Running the same
        source and destination again reuses that job and only uploads changes.
      </p>
      <ErrorBox error={start.error} />
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" loading={start.isPending}>
          <Zap /> Start backup
        </Button>
      </DialogFooter>
    </form>
  )
}
