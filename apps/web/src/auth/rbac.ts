import type { HighlandUser } from '@/api/client'

export function canMutate(user: HighlandUser | null | undefined): boolean {
  return user?.role === 'admin' || user?.role === 'operator'
}

export function isAdmin(user: HighlandUser | null | undefined): boolean {
  return user?.role === 'admin'
}

export function isViewer(user: HighlandUser | null | undefined): boolean {
  return user?.role === 'viewer'
}
