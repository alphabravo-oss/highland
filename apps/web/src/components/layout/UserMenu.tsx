import { ChevronDown, KeyRound, LogOut, User } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { HighlandUser } from '@/api/client'
import { useAppTranslation } from '@/i18n/useAppTranslation'

type UserMenuProps = {
  user: HighlandUser | null
  onLogout: () => void
}

export function UserMenu({ user, onLogout }: UserMenuProps) {
  const { t } = useAppTranslation()
  const navigate = useNavigate()
  const username = user?.username ?? '—'
  const role = user?.role
  const isAdmin = role === 'admin'

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          className="gap-1.5 px-2"
          aria-label={`User menu for ${username}`}
          data-testid="user-menu"
        >
          <User size={16} strokeWidth={1.75} className="shrink-0" />
          <span className="hidden max-w-[10rem] truncate sm:inline">{username}</span>
          {role ? (
            <span className="hidden rounded bg-[var(--color-muted)] px-1.5 py-0.5 text-xs font-normal text-[var(--color-muted-foreground)] sm:inline">
              {role}
            </span>
          ) : null}
          <ChevronDown size={14} strokeWidth={1.75} className="opacity-60" aria-hidden />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56" sideOffset={6}>
        <DropdownMenuLabel className="font-normal">
          <div className="flex flex-col gap-0.5">
            <span className="text-sm font-medium text-[var(--color-foreground)]">{username}</span>
            {role ? (
              <span className="text-xs font-normal text-[var(--color-muted-foreground)]">
                {t('admin.role')}: {t(`admin.roles.${role}`, { defaultValue: role })}
              </span>
            ) : null}
          </div>
        </DropdownMenuLabel>
        {isAdmin ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onSelect={() => navigate('/admin/sso')}
              data-testid="user-menu-sso"
            >
              <KeyRound size={16} strokeWidth={1.75} />
              {t('nav.sso')}
            </DropdownMenuItem>
          </>
        ) : null}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant="destructive"
          onSelect={() => onLogout()}
          data-testid="user-menu-logout"
        >
          <LogOut size={16} strokeWidth={1.75} />
          {t('auth.logout')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
