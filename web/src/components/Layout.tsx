import { Suspense, useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { Activity, HardDrive, LayoutDashboard, LogOut, Menu, Moon, Settings, Sun, Archive, X } from 'lucide-react'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui'
import { Loading } from '@/components/common'
import { QuickBackupButton } from '@/components/QuickBackup'
import { LogoMark } from '@/components/LogoMark'

const nav = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, end: true },
  { to: '/jobs', label: 'Backup jobs', icon: Archive },
  { to: '/storages', label: 'Storages', icon: HardDrive },
  { to: '/runs', label: 'Activity', icon: Activity },
  { to: '/settings', label: 'Settings', icon: Settings },
]

export function Logo({ large }: { large?: boolean }) {
  return (
    <div className={cn('flex items-center', large ? 'gap-3.5' : 'gap-2.5')}>
      <LogoMark className={cn('shrink-0 drop-shadow-sm', large ? 'size-12' : 'size-8')} />
      <span className={cn('font-semibold tracking-tight', large ? 'text-3xl' : 'text-[15px]')}>
        S3 <span className="bg-gradient-to-r from-sky-400 to-blue-600 bg-clip-text text-transparent">Freeze</span>
      </span>
    </div>
  )
}

function useTheme() {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains('dark'))
  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
    try {
      localStorage.setItem('theme', dark ? 'dark' : 'light')
    } catch {
      /* storage unavailable */
    }
  }, [dark])
  return [dark, setDark] as const
}

export default function Layout({ username }: { username: string }) {
  const [open, setOpen] = useState(false)
  const [dark, setDark] = useTheme()
  const location = useLocation()
  const qc = useQueryClient()

  useEffect(() => setOpen(false), [location.pathname])

  const logout = async () => {
    await api.post('/api/auth/logout')
    qc.clear()
    qc.invalidateQueries({ queryKey: ['auth'] })
  }

  const sidebar = (
    <div className="flex h-full flex-col gap-6 p-4">
      <div className="flex items-center justify-between px-2 pt-1">
        <Logo />
        <Button variant="ghost" size="icon-sm" className="lg:hidden" onClick={() => setOpen(false)}>
          <X />
        </Button>
      </div>
      <QuickBackupButton className="w-full" />
      <nav className="-mt-2 flex flex-col gap-0.5">
        {nav.map((n) => (
          <NavLink
            key={n.to}
            to={n.to}
            end={n.end}
            className={({ isActive }) =>
              cn(
                'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                isActive ? 'bg-accent text-accent-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
              )
            }
          >
            <n.icon className="size-4" />
            {n.label}
          </NavLink>
        ))}
      </nav>
      <div className="mt-auto flex items-center justify-between gap-2 border-t px-2 pt-4">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium">{username}</p>
          <p className="text-xs text-muted-foreground">Administrator</p>
        </div>
        <div className="flex">
          <Button variant="ghost" size="icon-sm" title="Toggle theme" onClick={() => setDark(!dark)}>
            {dark ? <Sun /> : <Moon />}
          </Button>
          <Button variant="ghost" size="icon-sm" title="Sign out" onClick={logout}>
            <LogOut />
          </Button>
        </div>
      </div>
    </div>
  )

  return (
    <div className="min-h-dvh">
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-60 border-r bg-card lg:block">{sidebar}</aside>

      {open && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <div className="absolute inset-0 bg-black/40" onClick={() => setOpen(false)} />
          <aside className="absolute inset-y-0 left-0 w-64 border-r bg-card shadow-xl">{sidebar}</aside>
        </div>
      )}

      <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b bg-card/80 px-4 backdrop-blur lg:hidden">
        <Button variant="ghost" size="icon-sm" onClick={() => setOpen(true)}>
          <Menu />
        </Button>
        <Logo />
      </header>

      <main className="lg:pl-60">
        <div className="mx-auto max-w-6xl px-4 py-6 sm:px-6 lg:py-8">
          <Suspense fallback={<Loading />}>
            <Outlet />
          </Suspense>
        </div>
      </main>
    </div>
  )
}
