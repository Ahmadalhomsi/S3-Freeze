import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Field, Input } from '@/components/ui'
import { ErrorBox, PageHeader } from '@/components/common'

export default function SettingsPage() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')

  const change = useMutation({
    mutationFn: () => {
      if (next !== confirm) throw new Error('New passwords do not match')
      return api.post('/api/auth/password', { current_password: current, new_password: next })
    },
    onSuccess: () => {
      toast.success('Password changed. Other sessions were signed out.')
      setCurrent('')
      setNext('')
      setConfirm('')
    },
  })

  return (
    <>
      <PageHeader title="Settings" />
      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Change password</CardTitle>
            <CardDescription>Other signed-in sessions will be logged out.</CardDescription>
          </CardHeader>
          <CardContent>
            <form
              className="flex flex-col gap-4"
              onSubmit={(e) => {
                e.preventDefault()
                change.mutate()
              }}
            >
              <Field label="Current password">
                <Input type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
              </Field>
              <Field label="New password" hint="At least 8 characters.">
                <Input type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} required minLength={8} />
              </Field>
              <Field label="Confirm new password">
                <Input type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
              </Field>
              <ErrorBox error={change.error} />
              <Button type="submit" loading={change.isPending} className="self-start">
                Update password
              </Button>
            </form>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>About backups</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 text-sm text-muted-foreground">
            <p>
              Each run creates a <strong className="text-foreground">snapshot</strong>: a manifest of every object in the source. Object
              contents are stored once, addressed by their hash, so unchanged and duplicate objects take no extra space.
            </p>
            <p>
              Repositories are self-describing. If you lose this server, deploy S3 Freeze again, create a job pointing at the same
              destination (with the same passphrase if encrypted), and use <strong className="text-foreground">Scan repository</strong> to
              bring the snapshots back.
            </p>
            <p>
              Stored credentials are encrypted with <code className="font-mono text-foreground">MASTER_KEY</code>. Keep that value safe —
              without it, saved storage credentials and passphrases cannot be decrypted.
            </p>
          </CardContent>
        </Card>
      </div>
    </>
  )
}
