export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 's3sync' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    if (res.status === 401 && !path.startsWith('/api/auth')) {
      window.dispatchEvent(new Event('s3sync:unauthorized'))
    }
    throw new ApiError(data?.error ?? res.statusText, res.status)
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
  put: <T>(path: string, body: unknown) => request<T>('PUT', path, body),
  del: <T>(path: string) => request<T>('DELETE', path),
}

export type StorageType = 's3' | 'local'

export interface Storage {
  id: number
  name: string
  type: StorageType
  endpoint: string
  region: string
  access_key: string
  use_ssl: boolean
  path_style: boolean
  local_path: string
  builtin: boolean
  has_secret: boolean
  created_at: string
  updated_at: string
}

export interface StorageInput {
  id?: number
  name: string
  type: StorageType
  endpoint: string
  region: string
  access_key: string
  secret_key: string
  use_ssl: boolean
  path_style: boolean
  local_path: string
}

export type RunKind = 'backup' | 'restore' | 'delete' | 'scan'
export type RunStatus = 'running' | 'success' | 'warning' | 'failed' | 'cancelled'

export interface Run {
  id: number
  job_id: number
  job_name: string
  kind: RunKind
  status: RunStatus
  detail: string
  started_at: string
  finished_at: string | null
  objects_total: number
  objects_done: number
  objects_failed: number
  bytes_total: number
  bytes_done: number
  bytes_uploaded: number
  snapshot_id: string
  error: string
  log?: string
}

export type Health = 'healthy' | 'failing' | 'warning' | 'stale' | 'never' | 'disabled' | 'running'

export interface JobInput {
  name: string
  enabled: boolean
  schedule: string
  source_storage_id: number
  source_bucket: string
  source_prefix: string
  dest_storage_id: number
  dest_bucket: string
  dest_prefix: string
  compression: boolean
  encryption: boolean
  passphrase: string
  concurrency: number
  keep_last: number
  keep_days: number
}

export interface Job extends Omit<JobInput, 'passphrase'> {
  id: number
  created_at: string
  updated_at: string
  has_passphrase: boolean
  health: Health
  next_run: string | null
  last_run: Run | null
  last_success: string | null
  active_run_id: number
  snapshots: {
    count: number
    latest_id: string
    latest_at: string | null
    latest_size: number
    latest_objects: number
  }
}

export interface Snapshot {
  id: string
  job_id: number
  run_id: number
  created_at: string
  objects: number
  size: number
  added_bytes: number
}

export interface Listing {
  path: string
  dirs: { name: string; path: string; objects: number; size: number }[]
  files: { name: string; path: string; size: number; modified: string; content_type: string }[]
}

export interface RestoreRequest {
  paths: string[]
  storage_id: number
  bucket: string
  prefix: string
  strip_prefix: string
  overwrite: boolean
}

export interface Dashboard {
  jobs: Job[]
  health: Partial<Record<Health, number>>
  storages: number
  protected_bytes: number
  protected_objects: number
  snapshots: number
  runs_24h: { success: number; warning: number; failed: number }
  active_runs: Run[]
  recent_runs: Run[]
}

export interface DiskInfo {
  path: string
  total: number
  free: number
}

export interface Usage {
  objects: number
  size: number
  partial: boolean
}

export interface QuickBackupInput {
  source_storage_id: number
  source_bucket: string
  source_prefix: string
  dest_storage_id: number
  dest_bucket: string
  dest_prefix: string
  encryption: boolean
  passphrase: string
}

export interface QuickBackupResult {
  job_id: number
  job_name: string
  run_id: number
  existing_job: boolean
}

export interface AuthStatus {
  setup_required: boolean
  authenticated: boolean
  username?: string
}
