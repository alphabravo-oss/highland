export type HighlandUser = {
  username: string
  role: string
}

export type MeResponse = {
  user: HighlandUser
}

const jsonHeaders = { 'Content-Type': 'application/json' }

export async function parseError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as {
      error?: string
      message?: string
      status?: string
    }
    return body.error ?? body.message ?? body.status ?? res.statusText
  } catch {
    return res.statusText
  }
}

/** Typed client for Highland API only — never calls Longhorn manager directly. */
export async function login(username: string, password: string): Promise<HighlandUser> {
  const res = await fetch('/auth/login', {
    method: 'POST',
    credentials: 'include',
    headers: jsonHeaders,
    body: JSON.stringify({ username, password }),
  })
  if (!res.ok) {
    throw new Error(await parseError(res))
  }
  const body = (await res.json()) as { user: HighlandUser }
  return body.user
}

export async function logout(): Promise<void> {
  await fetch('/auth/logout', {
    method: 'POST',
    credentials: 'include',
  })
}

export async function me(): Promise<HighlandUser | null> {
  const res = await fetch('/auth/me', {
    credentials: 'include',
  })
  if (res.status === 401) return null
  if (!res.ok) {
    throw new Error(await parseError(res))
  }
  const body = (await res.json()) as MeResponse
  return body.user
}

/**
 * Resolve a manager/link URL to a same-origin Highland path.
 * Accepts already-rewritten `/api/v1/lh/...` or relative `/v1/...` or absolute manager URLs.
 */
export function toHighlandPath(url: string): string {
  if (!url) return url
  if (url.startsWith('/api/v1/lh')) return url
  if (url.startsWith('/v1/') || url === '/v1') {
    return `/api/v1/lh${url.slice(3) || ''}`
  }
  try {
    const u = new URL(url, window.location.origin)
    if (u.pathname.startsWith('/v1')) {
      return `/api/v1/lh${u.pathname.slice(3)}${u.search}`
    }
    if (u.pathname.startsWith('/api/v1/lh')) {
      return `${u.pathname}${u.search}`
    }
  } catch {
    /* fall through */
  }
  // Relative path without leading slash
  if (url.startsWith('volumes') || url.startsWith('nodes') || url.startsWith('settings')) {
    return `/api/v1/lh/${url}`
  }
  return url
}

async function highlandFetch<T>(
  pathOrUrl: string,
  init?: RequestInit,
): Promise<T> {
  const path = toHighlandPath(pathOrUrl)
  const res = await fetch(path, {
    credentials: 'include',
    ...init,
    headers: {
      ...(init?.body ? jsonHeaders : {}),
      ...init?.headers,
    },
  })
  if (!res.ok) {
    throw new Error(await parseError(res))
  }
  if (res.status === 204) {
    return undefined as T
  }
  const ct = res.headers.get('Content-Type') ?? ''
  if (!ct.includes('json')) {
    return (await res.text()) as T
  }
  return (await res.json()) as T
}

export async function lhGet<T = unknown>(path: string): Promise<T> {
  const clean = path.startsWith('/') ? path : `/${path}`
  return highlandFetch<T>(`/api/v1/lh${clean}`)
}

export async function lhPost<T = unknown>(path: string, body?: unknown): Promise<T> {
  const clean = path.startsWith('/') ? path : `/${path}`
  return highlandFetch<T>(`/api/v1/lh${clean}`, {
    method: 'POST',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

export async function lhPut<T = unknown>(path: string, body?: unknown): Promise<T> {
  const clean = path.startsWith('/') ? path : `/${path}`
  return highlandFetch<T>(`/api/v1/lh${clean}`, {
    method: 'PUT',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

export async function lhDelete<T = unknown>(path: string): Promise<T> {
  const clean = path.startsWith('/') ? path : `/${path}`
  return highlandFetch<T>(`/api/v1/lh${clean}`, { method: 'DELETE' })
}

/** Highland-native API (not Longhorn proxy). */
export async function highlandGet<T = unknown>(path: string): Promise<T> {
  const clean = path.startsWith('/') ? path : `/${path}`
  return highlandFetch<T>(`/api/v1${clean}`)
}

export async function highlandPost<T = unknown>(path: string, body?: unknown): Promise<T> {
  const clean = path.startsWith('/') ? path : `/${path}`
  return highlandFetch<T>(`/api/v1${clean}`, {
    method: 'POST',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

export async function highlandPut<T = unknown>(path: string, body?: unknown): Promise<T> {
  const clean = path.startsWith('/') ? path : `/${path}`
  return highlandFetch<T>(`/api/v1${clean}`, {
    method: 'PUT',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

export async function highlandDelete<T = unknown>(path: string): Promise<T> {
  const clean = path.startsWith('/') ? path : `/${path}`
  return highlandFetch<T>(`/api/v1${clean}`, { method: 'DELETE' })
}

export async function oidcMockLogin(email: string, role: string): Promise<HighlandUser> {
  const res = await fetch('/auth/oidc/mock', {
    method: 'POST',
    credentials: 'include',
    headers: jsonHeaders,
    body: JSON.stringify({ email, role }),
  })
  if (!res.ok) throw new Error(await parseError(res))
  const body = (await res.json()) as { user: HighlandUser }
  return body.user
}

/** POST to an action/link URL returned by the manager (after rewrite). */
export async function lhRequest<T = unknown>(
  url: string,
  method: string,
  body?: unknown,
): Promise<T> {
  return highlandFetch<T>(url, {
    method,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}
