import { Navigate } from 'react-router-dom'
import { useAuth } from '@/auth/AuthContext'
import { AppShell } from '@/components/layout/AppShell'
import { useAppTranslation } from '@/i18n/useAppTranslation'

export function AuthenticatedLayout() {
  const { t } = useAppTranslation()
  const { user, loading, logout } = useAuth()

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-[var(--color-muted-foreground)]">
        {t('common.loading')}
      </div>
    )
  }

  if (!user) {
    return <Navigate to="/login" replace />
  }

  return (
    <AppShell
      user={user}
      onLogout={() => {
        void logout()
      }}
    />
  )
}
