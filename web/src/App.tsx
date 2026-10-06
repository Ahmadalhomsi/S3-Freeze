import { lazy, useEffect } from 'react'
import { Navigate, Route, Routes } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type AuthStatus } from '@/lib/api'
import { Loading } from '@/components/common'
import Layout from '@/components/Layout'
import AuthPage from '@/pages/Auth'
// Pages are loaded on demand to keep the initial download small.
const DashboardPage = lazy(() => import('@/pages/Dashboard'))
const StoragesPage = lazy(() => import('@/pages/Storages'))
const StorageDetailPage = lazy(() => import('@/pages/StorageDetail'))
const JobsPage = lazy(() => import('@/pages/Jobs'))
const JobFormPage = lazy(() => import('@/pages/JobForm'))
const JobDetailPage = lazy(() => import('@/pages/JobDetail'))
const SnapshotBrowserPage = lazy(() => import('@/pages/SnapshotBrowser'))
const RunsPage = lazy(() => import('@/pages/Runs'))
const RunDetailPage = lazy(() => import('@/pages/RunDetail'))
const SettingsPage = lazy(() => import('@/pages/Settings'))
import { QuickBackupProvider } from '@/components/QuickBackup'

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
      <QuickBackupProvider>
      <Routes>
        <Route element={<Layout username={auth.username ?? ''} />}>
          <Route index element={<DashboardPage />} />
          <Route path="jobs" element={<JobsPage />} />
          <Route path="jobs/new" element={<JobFormPage />} />
          <Route path="jobs/:id" element={<JobDetailPage />} />
          <Route path="jobs/:id/edit" element={<JobFormPage />} />
          <Route path="jobs/:id/snapshots/:sid" element={<SnapshotBrowserPage />} />
          <Route path="storages" element={<StoragesPage />} />
          <Route path="storages/:id" element={<StorageDetailPage />} />
          <Route path="runs" element={<RunsPage />} />
          <Route path="runs/:id" element={<RunDetailPage />} />
          <Route path="settings" element={<SettingsPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
    </QuickBackupProvider>
  )
}
