import { useQuery } from '@tanstack/react-query'
import { storageClient } from './client'
import type { StorageFilters, StoragePage } from './types'

export const storageKeys = {
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
  return useQuery({ queryKey: [...storageKeys.provider(provider), 'summary'], queryFn: () => storageClient.summary<T>(provider), enabled: Boolean(provider) })
}

export function useStorageProviders() {
  return useQuery({ queryKey: storageKeys.providers(), queryFn: storageClient.providers, refetchInterval: 30_000 })
}

export function useStorageProvider(id: string) {
  return useQuery({ queryKey: storageKeys.provider(id), queryFn: () => storageClient.provider(id), enabled: Boolean(id) })
}

type ListKind = 'drivers' | 'classes' | 'claims' | 'volumes' | 'snapshots' | 'attachments' | 'capacity' | 'events'

export function useStorageList<T>(kind: ListKind, filters: StorageFilters) {
  return useQuery<StoragePage<T>>({
    queryKey: storageKeys.list(kind, filters),
    queryFn: () => storageClient[kind](filters) as Promise<StoragePage<T>>,
    placeholderData: (previous) => previous,
    refetchInterval: 30_000,
  })
}

export function useStorageClaim(namespace: string, name: string) {
  return useQuery({ queryKey: storageKeys.claim(namespace, name), queryFn: () => storageClient.claim(namespace, name), enabled: Boolean(namespace && name), refetchInterval: 30_000 })
}

export function useStorageVolume(name: string) {
  return useQuery({ queryKey: storageKeys.volume(name), queryFn: () => storageClient.volume(name), enabled: Boolean(name), refetchInterval: 30_000 })
}

export function useProviderResources<T>(provider: string, kind: string, filters: StorageFilters) {
  return useQuery({
    queryKey: storageKeys.resources(provider, kind, filters),
    queryFn: () => storageClient.resources<T>(provider, kind, filters),
    enabled: Boolean(provider && kind),
  })
}

export function useProviderResource<T>(provider: string, kind: string, id: string) {
  return useQuery({
    queryKey: storageKeys.resource(provider, kind, id),
    queryFn: () => storageClient.resource<T>(provider, kind, id),
    enabled: Boolean(provider && kind && id),
  })
}

export function useStorageActions() {
  return useQuery({ queryKey: [...storageKeys.root, 'actions'], queryFn: storageClient.actions })
}

export function useStorageOperations(filters: StorageFilters & { action?: string; state?: string; user?: string }) {
  return useQuery({ queryKey: [...storageKeys.root, 'operations', filters], queryFn: () => storageClient.operations(filters), refetchInterval: 3_000 })
}

export function useStorageOperation(id: string) {
  return useQuery({ queryKey: [...storageKeys.root, 'operations', id], queryFn: () => storageClient.operation(id), enabled: Boolean(id), refetchInterval: (query) => ['Succeeded', 'Failed', 'Cancelled'].includes(query.state.data?.status.phase ?? '') ? false : 2_000 })
}
