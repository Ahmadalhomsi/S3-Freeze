import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Field, Input } from '@/components/ui'
import { ErrorBox } from '@/components/common'
import { Logo } from '@/components/Layout'

export default function AuthPage({ setup }: { setup: boolean }) {
  const qc = useQueryClient()
  const [username, setUsername] = useState(setup ? 'admin' : '')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [setupToken, setSetupToken] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    if (setup && password !== confirm) {
      setError(new Error('Passwords do not match'))
      return
    }
    setBusy(true)
    try {
      await api.post(setup ? '/api/auth/setup' : '/api/auth/login', setup ? { username, password, setup_token: setupToken } : { username, password })
      await qc.invalidateQueries({ queryKey: ['auth'] })
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-dvh items-center justify-center bg-gradient-to-b from-accent/40 to-background px-4">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex justify-center">
          <Logo large />
        </div>
        <Card>
          <CardHeader>
            <CardTitle>{setup ? 'Create your admin account' : 'Sign in'}</CardTitle>
            <CardDescription>
              {setup ? 'This account manages all storages, jobs and restores.' : 'Welcome back. Sign in to manage your backups.'}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={submit} className="flex flex-col gap-4">
              {setup && (
                <Field
                  label="Setup token"
                  htmlFor="token"
                  hint="Printed in the server logs on first start (Coolify → Logs, or docker logs). Proves you own this server."
                >
                  <Input id="token" className="font-mono" autoComplete="off" value={setupToken} onChange={(e) => setSetupToken(e.target.value)} required autoFocus />
                </Field>
              )}
              <Field label="Username" htmlFor="username">
                <Input id="username" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required autoFocus={!setup} />
              </Field>
              <Field label="Password" htmlFor="password" hint={setup ? 'At least 8 characters.' : undefined}>
                <Input
                  id="password"
                  type="password"
                  autoComplete={setup ? 'new-password' : 'current-password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                />
              </Field>
              {setup && (
                <Field label="Confirm password" htmlFor="confirm">
                  <Input id="confirm" type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
                </Field>
              )}
              <ErrorBox error={error} />
              <Button type="submit" loading={busy} className="mt-1">
                {setup ? 'Create account' : 'Sign in'}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
