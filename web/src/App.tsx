import { useEffect } from 'react'
import { Navigate, Route, Routes } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type AuthStatus } from '@/lib/api'
import { Loading } from '@/components/common'
import Layout from '@/components/Layout'
import AuthPage from '@/pages/Auth'
import DashboardPage from '@/pages/Dashboard'
import StoragesPage from '@/pages/Storages'
import JobsPage from '@/pages/Jobs'
import JobFormPage from '@/pages/JobForm'
import JobDetailPage from '@/pages/JobDetail'
import SnapshotBrowserPage from '@/pages/SnapshotBrowser'
import RunsPage from '@/pages/Runs'
import RunDetailPage from '@/pages/RunDetail'
import SettingsPage from '@/pages/Settings'

export default function App() {
  const qc = useQueryClient()
  const { data: auth, isLoading } = useQuery({
    queryKey: ['auth'],
    queryFn: () => api.get<AuthStatus>('/api/auth/status'),
    staleTime: 60_000,
  })

  useEffect(() => {
    const onUnauthorized = () => qc.invalidateQueries({ queryKey: ['auth'] })
    window.addEventListener('s3sync:unauthorized', onUnauthorized)
    return () => window.removeEventListener('s3sync:unauthorized', onUnauthorized)
  }, [qc])

  if (isLoading || !auth) return <Loading />
  if (!auth.authenticated) return <AuthPage setup={auth.setup_required} />

  return (
    <Routes>
      <Route element={<Layout username={auth.username ?? ''} />}>
        <Route index element={<DashboardPage />} />
        <Route path="jobs" element={<JobsPage />} />
        <Route path="jobs/new" element={<JobFormPage />} />
        <Route path="jobs/:id" element={<JobDetailPage />} />
        <Route path="jobs/:id/edit" element={<JobFormPage />} />
        <Route path="jobs/:id/snapshots/:sid" element={<SnapshotBrowserPage />} />
        <Route path="storages" element={<StoragesPage />} />
        <Route path="runs" element={<RunsPage />} />
        <Route path="runs/:id" element={<RunDetailPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
