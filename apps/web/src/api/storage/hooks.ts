import { useQuery } from '@tanstack/react-query'
import { storageClient } from './client'
import type { StorageFilters, StoragePage } from './types'
import { useSseConnected } from '@/api/realtime'

const storageKeys = {
  root: ['storage'] as const,
  providers: () => [...storageKeys.root, 'providers'] as const,
  provider: (id: string) => [...storageKeys.providers(), id] as const,
  list: (kind: string, filters: StorageFilters) => [...storageKeys.root, kind, filters] as const,
  resources: (provider: string, kind: string, filters: StorageFilters) =>
    [...storageKeys.root, 'provider-resources', provider, kind, filters] as const,
  resource: (provider: string, kind: string, id: string) =>
    [...storageKeys.root, 'provider-resources', provider, kind, id] as const,
  claim: (namespace: string, name: string) => [...storageKeys.root, 'claims', namespace, name] as const,
  volume: (name: string) => [...storageKeys.root, 'volumes', name] as const,
}

export function useProviderSummary<T>(provider: string) {
  return useQuery({ queryKey: [...storageKeys.provider(provider), 'summary'], queryFn: ({ signal }) => storageClient.summary<T>(provider, signal), enabled: Boolean(provider) })
}

export function useStorageProviders() {
  const connected = useSseConnected()
  return useQuery({
    queryKey: storageKeys.providers(),
    queryFn: ({ signal }) => storageClient.providers(signal),
    staleTime: 15_000,
    refetchInterval: (query) => query.state.data?.meta.stale ? 10_000 : connected ? 60_000 : 30_000,
  })
}

export function useStorageProvider(id: string) {
  return useQuery({ queryKey: storageKeys.provider(id), queryFn: ({ signal }) => storageClient.provider(id, signal), enabled: Boolean(id) })
}

type ListKind = 'drivers' | 'classes' | 'claims' | 'volumes' | 'snapshots' | 'attachments' | 'capacity' | 'events'

export function useStorageList<T>(kind: ListKind, filters: StorageFilters) {
  const connected = useSseConnected()
  return useQuery<StoragePage<T>>({
    queryKey: storageKeys.list(kind, filters),
    queryFn: ({ signal }) => storageClient[kind](filters, signal) as Promise<StoragePage<T>>,
    placeholderData: (previous) => previous,
    refetchInterval: connected ? 60_000 : 30_000,
  })
}

export function useStorageClaim(namespace: string, name: string) {
  const connected = useSseConnected()
  return useQuery({ queryKey: storageKeys.claim(namespace, name), queryFn: ({ signal }) => storageClient.claim(namespace, name, signal), enabled: Boolean(namespace && name), refetchInterval: connected ? 60_000 : 30_000 })
}

export function useStorageVolume(name: string) {
  const connected = useSseConnected()
  return useQuery({ queryKey: storageKeys.volume(name), queryFn: ({ signal }) => storageClient.volume(name, signal), enabled: Boolean(name), refetchInterval: connected ? 60_000 : 30_000 })
}

export function useProviderResources<T>(provider: string, kind: string, filters: StorageFilters) {
  return useQuery({
    queryKey: storageKeys.resources(provider, kind, filters),
    queryFn: ({ signal }) => storageClient.resources<T>(provider, kind, filters, signal),
    enabled: Boolean(provider && kind),
  })
}

export function useProviderResource<T>(provider: string, kind: string, id: string) {
  return useQuery({
    queryKey: storageKeys.resource(provider, kind, id),
    queryFn: ({ signal }) => storageClient.resource<T>(provider, kind, id, signal),
    enabled: Boolean(provider && kind && id),
  })
}

export function useStorageActions() {
  return useQuery({ queryKey: [...storageKeys.root, 'actions'], queryFn: ({ signal }) => storageClient.actions(signal) })
}

export function useStorageOperations(filters: StorageFilters & { action?: string; state?: string; user?: string }) {
  const connected = useSseConnected()
  return useQuery({
    queryKey: [...storageKeys.root, 'operations', filters],
    queryFn: ({ signal }) => storageClient.operations(filters, signal),
    refetchInterval: (query) => {
      const active = query.state.data?.data.some((operation) => !['Succeeded', 'Failed', 'Cancelled'].includes(operation.status.phase))
      if (active && !connected) return 3_000
      return connected ? 60_000 : 30_000
    },
  })
}

export function useStorageOperation(id: string) {
  const connected = useSseConnected()
  return useQuery({
    queryKey: [...storageKeys.root, 'operations', id],
    queryFn: ({ signal }) => storageClient.operation(id, signal),
    enabled: Boolean(id),
    refetchInterval: (query) => {
      if (['Succeeded', 'Failed', 'Cancelled'].includes(query.state.data?.status.phase ?? '')) return false
      return connected ? 60_000 : 2_000
    },
  })
}
