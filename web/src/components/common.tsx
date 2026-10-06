import * as React from 'react'
import { useState } from 'react'
import {
  AlertTriangle,
  Ban,
  CheckCircle2,
  CircleDashed,
  Clock,
  Loader2,
  PauseCircle,
  XCircle,
  type LucideIcon,
} from 'lucide-react'
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, Progress } from '@/components/ui'
import type { Health, Run, RunStatus } from '@/lib/api'
import { formatBytes, formatNumber, percent } from '@/lib/utils'

export function PageHeader({
  title,
  description,
  actions,
  children,
}: {
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  children?: React.ReactNode
}) {
  return (
    <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
      <div className="min-w-0">
        {children}
        <h1 className="truncate text-2xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  )
}

export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
}: {
  icon: LucideIcon
  title: string
  description?: string
  action?: React.ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 px-6 py-14 text-center">
      <div className="rounded-full bg-muted p-3">
        <Icon className="size-6 text-muted-foreground" />
      </div>
      <div>
        <p className="font-medium">{title}</p>
        {description && <p className="mt-1 max-w-sm text-sm text-muted-foreground">{description}</p>}
      </div>
      {action}
    </div>
  )
}

const healthMeta: Record<Health, { label: string; variant: 'success' | 'warning' | 'destructive' | 'default' | 'primary' | 'outline'; icon: LucideIcon }> = {
  healthy: { label: 'Healthy', variant: 'success', icon: CheckCircle2 },
  warning: { label: 'Warnings', variant: 'warning', icon: AlertTriangle },
  stale: { label: 'Stale', variant: 'warning', icon: Clock },
  failing: { label: 'Failing', variant: 'destructive', icon: XCircle },
  never: { label: 'No backups yet', variant: 'outline', icon: CircleDashed },
  disabled: { label: 'Paused', variant: 'default', icon: PauseCircle },
  running: { label: 'Running', variant: 'primary', icon: Loader2 },
}

export function HealthBadge({ health }: { health: Health }) {
  const m = healthMeta[health] ?? healthMeta.never
  return (
    <Badge variant={m.variant}>
      <m.icon className={health === 'running' ? 'animate-spin' : ''} />
      {m.label}
    </Badge>
  )
}

const statusMeta: Record<RunStatus, { label: string; variant: 'success' | 'warning' | 'destructive' | 'default' | 'primary'; icon: LucideIcon }> = {
  running: { label: 'Running', variant: 'primary', icon: Loader2 },
  success: { label: 'Success', variant: 'success', icon: CheckCircle2 },
  warning: { label: 'Warnings', variant: 'warning', icon: AlertTriangle },
  failed: { label: 'Failed', variant: 'destructive', icon: XCircle },
  cancelled: { label: 'Cancelled', variant: 'default', icon: Ban },
}

export function StatusBadge({ status }: { status: RunStatus }) {
  const m = statusMeta[status] ?? statusMeta.failed
  return (
    <Badge variant={m.variant}>
      <m.icon className={status === 'running' ? 'animate-spin' : ''} />
      {m.label}
    </Badge>
  )
}

export const kindLabel: Record<string, string> = {
  backup: 'Backup',
  restore: 'Restore',
  delete: 'Delete snapshot',
  scan: 'Repository scan',
}

export function RunProgress({ run }: { run: Run }) {
  const useBytes = run.bytes_total > 0
  const p = useBytes ? percent(run.bytes_done, run.bytes_total) : percent(run.objects_done, run.objects_total)
  return (
    <div className="flex flex-col gap-1.5">
      <Progress value={p} active={run.status === 'running'} />
      <div className="flex justify-between gap-2 text-xs text-muted-foreground tabular-nums">
        <span>
          {formatNumber(run.objects_done)} / {formatNumber(run.objects_total)} objects
          {run.objects_failed > 0 && <span className="text-destructive"> · {run.objects_failed} failed</span>}
        </span>
        <span>
          {formatBytes(run.bytes_done)} / {formatBytes(run.bytes_total)} · {p}%
        </span>
      </div>
    </div>
  )
}

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = 'Confirm',
  destructive,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  title: string
  description: React.ReactNode
  confirmLabel?: string
  destructive?: boolean
  onConfirm: () => Promise<unknown> | void
}) {
  const [busy, setBusy] = useState(false)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription asChild>
            <div>{description}</div>
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            variant={destructive ? 'destructive' : 'default'}
            loading={busy}
            onClick={async () => {
              setBusy(true)
              try {
                await onConfirm()
                onOpenChange(false)
              } finally {
                setBusy(false)
              }
            }}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function Stat({
  icon: Icon,
  label,
  value,
  sub,
}: {
  icon: LucideIcon
  label: string
  value: React.ReactNode
  sub?: React.ReactNode
}) {
  return (
    <div className="rounded-xl border bg-card p-5 shadow-xs">
      <div className="flex items-center justify-between">
        <p className="text-sm font-medium text-muted-foreground">{label}</p>
        <Icon className="size-4 text-muted-foreground" />
      </div>
      <p className="mt-2 text-2xl font-semibold tracking-tight tabular-nums">{value}</p>
      {sub && <div className="mt-1 text-xs text-muted-foreground">{sub}</div>}
    </div>
  )
}

export function Loading() {
  return (
    <div className="flex items-center justify-center py-20 text-muted-foreground">
      <Loader2 className="size-5 animate-spin" />
    </div>
  )
}

export function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null
  return (
    <div className="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      <XCircle className="mt-0.5 size-4 shrink-0" />
      <span className="break-words">{error instanceof Error ? error.message : String(error)}</span>
    </div>
  )
}
