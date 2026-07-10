import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import {
  login as apiLogin,
  logout as apiLogout,
  me,
  oidcMockLogin,
  type HighlandUser,
} from '@/api/client'
import { canMutate, isAdmin } from '@/auth/rbac'

type AuthContextValue = {
  user: HighlandUser | null
  loading: boolean
  login: (username: string, password: string) => Promise<void>
  loginOidcMock: (email: string, role: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
  canMutate: boolean
  isAdmin: boolean
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<HighlandUser | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      const u = await me()
      setUser(u)
    } catch {
      setUser(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const login = useCallback(async (username: string, password: string) => {
    const u = await apiLogin(username, password)
    setUser(u)
  }, [])

  const loginOidcMock = useCallback(async (email: string, role: string) => {
    const u = await oidcMockLogin(email, role)
    setUser(u)
  }, [])

  const logout = useCallback(async () => {
    await apiLogout()
    setUser(null)
  }, [])

  const value = useMemo(
    () => ({
      user,
      loading,
      login,
      loginOidcMock,
      logout,
      refresh,
      canMutate: canMutate(user),
      isAdmin: isAdmin(user),
    }),
    [user, loading, login, loginOidcMock, logout, refresh],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return ctx
}
