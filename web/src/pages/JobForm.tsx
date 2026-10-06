import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, KeyRound, TriangleAlert } from 'lucide-react'
import { toast } from 'sonner'
import { api, type Job, type JobInput, type Storage } from '@/lib/api'
import { cn, defaultFolder } from '@/lib/utils'
import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Field, Input, SwitchField } from '@/components/ui'
import { LocationPicker } from '@/components/LocationPicker'
import { ErrorBox, Loading, PageHeader } from '@/components/common'

const schedulePresets = [
  { value: '', label: 'Manual only' },
  { value: '@hourly', label: 'Every hour' },
  { value: '@every 6h', label: 'Every 6 hours' },
  { value: '0 2 * * *', label: 'Daily at 02:00' },
  { value: '0 3 * * 0', label: 'Weekly (Sunday 03:00)' },
  { value: '0 4 1 * *', label: 'Monthly (1st, 04:00)' },
]

const defaults: JobInput = {
  name: '',
  enabled: true,
  schedule: '0 2 * * *',
  source_storage_id: 0,
  source_bucket: '',
  source_prefix: '',
  dest_storage_id: 0,
  dest_bucket: '',
  dest_prefix: '',
  compression: true,
  encryption: false,
  passphrase: '',
  concurrency: 4,
  keep_last: 7,
  keep_days: 0,
}

function toInput(j: Job): JobInput {
  return {
    name: j.name,
    enabled: j.enabled,
    schedule: j.schedule,
    source_storage_id: j.source_storage_id,
    source_bucket: j.source_bucket,
    source_prefix: j.source_prefix,
    dest_storage_id: j.dest_storage_id,
    dest_bucket: j.dest_bucket,
    dest_prefix: j.dest_prefix,
    compression: j.compression,
    encryption: j.encryption,
    passphrase: '',
    concurrency: j.concurrency,
    keep_last: j.keep_last,
    keep_days: j.keep_days,
  }
}

export default function JobFormPage() {
  const { id } = useParams()
  const editing = !!id
  const { data: job, isLoading } = useQuery({
    queryKey: ['job', id],
    queryFn: () => api.get<Job>(`/api/jobs/${id}`),
    enabled: editing,
  })
  const { data: storages, isLoading: loadingStorages } = useQuery({
    queryKey: ['storages'],
    queryFn: () => api.get<Storage[]>('/api/storages'),
  })

  if ((editing && isLoading) || loadingStorages) return <Loading />
  return <JobForm job={job} storages={storages ?? []} />
}

function JobForm({ job, storages }: { job?: Job; storages: Storage[] }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [form, setForm] = useState<JobInput>(() =>
    job
      ? toInput(job)
      : {
          ...defaults,
          source_storage_id: (storages.find((s) => !s.builtin && s.type === 's3') ?? storages.find((s) => !s.builtin) ?? storages[0])?.id ?? 0,
          dest_storage_id: (storages.find((s) => s.builtin) ?? storages[1] ?? storages[0])?.id ?? 0,
        },
  )
  const set = <K extends keyof JobInput>(k: K, v: JobInput[K]) => setForm((f) => ({ ...f, [k]: v }))

  // New jobs name the destination folder after the source until it is edited.
  const [folderTouched, setFolderTouched] = useState(!!job)
  useEffect(() => {
    if (folderTouched) return
    const name = storages.find((s) => s.id === form.source_storage_id)?.name
    setForm((f) => ({ ...f, dest_prefix: defaultFolder(name, f.source_bucket) }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.source_storage_id, form.source_bucket, folderTouched])
  const isPreset = schedulePresets.some((p) => p.value === form.schedule)
  const [customSchedule, setCustomSchedule] = useState(!isPreset)

  const save = useMutation({
    mutationFn: () => (job ? api.put<Job>(`/api/jobs/${job.id}`, form) : api.post<Job>('/api/jobs', form)),
    onSuccess: (saved) => {
      qc.invalidateQueries()
      toast.success(job ? 'Job updated' : 'Backup job created')
      navigate(`/jobs/${saved.id}`)
    },
  })

  if (!storages.some((s) => !s.builtin)) {
    return (
      <>
        <PageHeader title="New backup job" />
        <Card className="p-6 text-sm">
          You need at least one storage before creating a job.{' '}
          <Link to="/storages" className="font-medium text-primary hover:underline">
            Add a storage
          </Link>
        </Card>
      </>
    )
  }

  const encryptionChanged = !!job && job.encryption !== form.encryption

  return (
    <>
      <PageHeader title={job ? `Edit “${job.name}”` : 'New backup job'}>
        <Link to={job ? `/jobs/${job.id}` : '/jobs'} className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" /> Back
        </Link>
      </PageHeader>

      <form
        className="grid gap-6 lg:grid-cols-[1fr_320px]"
        onSubmit={(e) => {
          e.preventDefault()
          save.mutate()
        }}
      >
        <div className="flex flex-col gap-6">
          <Card>
            <CardHeader>
              <CardTitle>General</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <Field label="Name">
                <Input value={form.name} onChange={(e) => set('name', e.target.value)} placeholder="Media bucket" required />
              </Field>
              <SwitchField
                label="Enabled"
                description="Paused jobs keep their snapshots but are not run on schedule."
                checked={form.enabled}
                onCheckedChange={(v) => set('enabled', v)}
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Source</CardTitle>
              <CardDescription>The whole storage, or one bucket and optionally a folder inside it.</CardDescription>
            </CardHeader>
            <CardContent>
              <LocationPicker
                storages={storages}
                value={{ storage_id: form.source_storage_id, bucket: form.source_bucket, prefix: form.source_prefix }}
                onChange={(v) => setForm((f) => ({ ...f, source_storage_id: v.storage_id, source_bucket: v.bucket, source_prefix: v.prefix }))}
                scope
                showSize
                folderHint="Only objects under this folder are included."
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Destination repository</CardTitle>
              <CardDescription>Where snapshots are stored. Several jobs may share one repository to deduplicate across them.</CardDescription>
            </CardHeader>
            <CardContent>
              <LocationPicker
                storages={storages}
                value={{ storage_id: form.dest_storage_id, bucket: form.dest_bucket, prefix: form.dest_prefix }}
                onChange={(v) => {
                  const switched = v.storage_id !== form.dest_storage_id || v.bucket !== form.dest_bucket
                  if (!switched && v.prefix !== form.dest_prefix) setFolderTouched(true)
                  setForm((f) => ({ ...f, dest_storage_id: v.storage_id, dest_bucket: v.bucket, dest_prefix: switched ? f.dest_prefix : v.prefix }))
                }}
                folderHint="Folder that holds the repository. Named after the source by default."
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Encryption</CardTitle>
              <CardDescription>Client-side AES-256-GCM. Data is encrypted before it leaves this server.</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <SwitchField
                label="Encrypt backups"
                description="Object contents, names and metadata are all encrypted."
                checked={form.encryption}
                onCheckedChange={(v) => set('encryption', v)}
              />
              {form.encryption && (
                <>
                  <Field
                    label="Passphrase"
                    hint={job?.has_passphrase ? 'Leave empty to keep the current passphrase.' : 'At least 8 characters.'}
                  >
                    <div className="relative">
                      <KeyRound className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                      <Input
                        type="password"
                        className="pl-9"
                        value={form.passphrase}
                        onChange={(e) => set('passphrase', e.target.value)}
                        placeholder={job?.has_passphrase ? '•••••••• (unchanged)' : ''}
                        autoComplete="new-password"
                        required={!job?.has_passphrase}
                        minLength={8}
                      />
                    </div>
                  </Field>
                  <div className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
                    <TriangleAlert className="mt-0.5 size-4 shrink-0" />
                    <span>
                      Store this passphrase somewhere safe (e.g. a password manager). Without it, backups cannot be restored — not even
                      by reinstalling S3 Sync.
                    </span>
                  </div>
                </>
              )}
              {encryptionChanged && (
                <div className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
                  <TriangleAlert className="mt-0.5 size-4 shrink-0" />
                  <span>
                    An existing repository cannot switch encryption on or off. Change the destination prefix to start a new repository.
                  </span>
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        <div className="flex flex-col gap-6">
          <Card>
            <CardHeader>
              <CardTitle>Schedule</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="flex flex-col gap-1">
                {schedulePresets.map((p) => (
                  <RadioRow
                    key={p.label}
                    checked={!customSchedule && form.schedule === p.value}
                    onChange={() => {
                      setCustomSchedule(false)
                      set('schedule', p.value)
                    }}
                    label={p.label}
                  />
                ))}
                <RadioRow checked={customSchedule} onChange={() => setCustomSchedule(true)} label="Custom cron expression" />
              </div>
              {customSchedule && (
                <Field label="Cron expression" hint="5 fields: minute hour day month weekday. Also @daily, @every 30m…">
                  <Input className="font-mono" value={form.schedule} onChange={(e) => set('schedule', e.target.value)} placeholder="30 1 * * *" />
                </Field>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Retention</CardTitle>
              <CardDescription>A snapshot is kept if it matches either rule. The newest one is always kept.</CardDescription>
            </CardHeader>
            <CardContent className="grid grid-cols-2 gap-4">
              <Field label="Keep last" hint="snapshots (0 = off)">
                <Input type="number" min={0} value={form.keep_last} onChange={(e) => set('keep_last', Number(e.target.value))} />
              </Field>
              <Field label="Keep for" hint="days (0 = off)">
                <Input type="number" min={0} value={form.keep_days} onChange={(e) => set('keep_days', Number(e.target.value))} />
              </Field>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Performance</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <SwitchField
                label="Compression"
                description="zstd (fast). Disable for already-compressed media."
                checked={form.compression}
                onCheckedChange={(v) => set('compression', v)}
              />
              <Field label="Parallel transfers" hint="Objects processed at the same time (1–64).">
                <Input type="number" min={1} max={64} value={form.concurrency} onChange={(e) => set('concurrency', Number(e.target.value))} />
              </Field>
            </CardContent>
          </Card>

          <div className="flex flex-col gap-3 lg:sticky lg:top-6">
            <ErrorBox error={save.error} />
            <Button type="submit" size="lg" loading={save.isPending}>
              {job ? 'Save changes' : 'Create job'}
            </Button>
          </div>
        </div>
      </form>
    </>
  )
}

function RadioRow({ checked, onChange, label }: { checked: boolean; onChange: () => void; label: string }) {
  return (
    <label
      className={cn(
        'flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors',
        checked ? 'bg-accent text-accent-foreground' : 'hover:bg-muted',
      )}
    >
      <input type="radio" className="accent-[var(--primary)]" checked={checked} onChange={onChange} />
      {label}
    </label>
  )
}

