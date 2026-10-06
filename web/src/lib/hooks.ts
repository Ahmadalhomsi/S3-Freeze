import { useQuery } from '@tanstack/react-query'
import { api, type DiskInfo, type Storage, type Usage } from '@/lib/api'

export function useStorages() {
  return useQuery({ queryKey: ['storages'], queryFn: () => api.get<Storage[]>('/api/storages') })
}

export function useBuckets(storageId: number) {
  return useQuery({
    queryKey: ['buckets', storageId],
    queryFn: () => api.get<string[]>(`/api/storages/${storageId}/buckets`),
    enabled: !!storageId,
    retry: false,
    staleTime: 60_000,
  })
}

/** Subfolders directly under the folder containing `prefix`. */
export function useFolders(storageId: number, bucket: string, prefix: string, enabled = true) {
  const parent = prefix.slice(0, prefix.lastIndexOf('/') + 1)
  return useQuery({
    queryKey: ['folders', storageId, bucket, parent],
    queryFn: () =>
      api.get<string[]>(
        `/api/storages/${storageId}/folders?bucket=${encodeURIComponent(bucket)}&prefix=${encodeURIComponent(parent)}`,
      ),
    enabled: enabled && !!storageId,
    retry: false,
    staleTime: 60_000,
    placeholderData: (prev) => prev,
  })
}

export function useUsage(storageId: number, bucket: string, prefix = '', enabled = true) {
  return useQuery({
    queryKey: ['usage', storageId, bucket, prefix],
    queryFn: () =>
      api.get<Usage>(
        `/api/storages/${storageId}/usage?bucket=${encodeURIComponent(bucket)}&prefix=${encodeURIComponent(prefix)}`,
      ),
    enabled: enabled && !!storageId,
    retry: false,
    staleTime: 5 * 60_000,
  })
}

/** Free space of a local-disk storage. */
export function useDisk(storageId: number, enabled = true) {
  return useQuery({
    queryKey: ['disk', storageId],
    queryFn: () => api.get<DiskInfo>(`/api/storages/${storageId}/disk`),
    enabled: enabled && !!storageId,
    retry: false,
    staleTime: 30_000,
  })
}
