import { Link, NavLink, Outlet, useNavigate } from 'react-router'
import { useTheme } from '@/theme/theme'
import { Code2, LogOut, Monitor, Moon, Shield, Sun, User as UserIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useAuth } from '@/auth/useAuth'
import { cn } from '@/lib/utils'

const navClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'text-sm transition-colors hover:text-foreground',
    isActive ? 'text-foreground font-medium' : 'text-muted-foreground',
  )

export function AppLayout() {
  const { state, logout } = useAuth()
  const { theme, setTheme } = useTheme()
  const navigate = useNavigate()
  const user = state.status === 'signedIn' ? state.user : null

  return (
    <div className="flex min-h-svh flex-col">
      <header className="sticky top-0 z-40 border-b bg-background/95 backdrop-blur">
        <div className="flex h-14 items-center gap-6 px-4">
          <Link to="/problems" className="flex items-center gap-2 font-semibold">
            <Code2 className="size-5" />
            Mini Code Judge
          </Link>
          <nav className="flex items-center gap-5">
            <NavLink to="/problems" className={navClass}>
              Problems
            </NavLink>
            <NavLink to="/leaderboard" className={navClass}>
              Leaderboard
            </NavLink>
            {user && (
              <NavLink to="/submissions" className={navClass}>
                Submissions
              </NavLink>
            )}
            {user?.role === 'admin' && (
              <NavLink to="/admin" className={navClass}>
                Admin
              </NavLink>
            )}
          </nav>
          <div className="ml-auto flex items-center gap-2">
            {state.status === 'signedOut' && (
              <>
                <Button variant="ghost" size="sm" asChild>
                  <Link to="/login">Sign in</Link>
                </Button>
                <Button size="sm" asChild>
                  <Link to="/register">Create account</Link>
                </Button>
              </>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" aria-label={user ? 'Account menu' : 'Theme'}>
                  {user ? <UserIcon /> : theme === 'dark' ? <Moon /> : <Sun />}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                {user && (
                  <>
                    <DropdownMenuLabel>
                      <div className="font-medium">{user.display_name || user.username}</div>
                      <div className="text-xs font-normal text-muted-foreground">{user.email}</div>
                    </DropdownMenuLabel>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem onSelect={() => navigate('/profile')}>
                      <UserIcon /> Profile & devices
                    </DropdownMenuItem>
                    {user.role === 'admin' && (
                      <DropdownMenuItem onSelect={() => navigate('/admin')}>
                        <Shield /> Admin
                      </DropdownMenuItem>
                    )}
                    <DropdownMenuSeparator />
                  </>
                )}
                <DropdownMenuLabel className="text-xs text-muted-foreground">Theme</DropdownMenuLabel>
                <DropdownMenuRadioGroup
                  value={theme}
                  onValueChange={(v) => setTheme(v as 'light' | 'dark' | 'system')}
                >
                  <DropdownMenuRadioItem value="light">
                    <Sun /> Light
                  </DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="dark">
                    <Moon /> Dark
                  </DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="system">
                    <Monitor /> System
                  </DropdownMenuRadioItem>
                </DropdownMenuRadioGroup>
                {user && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      onSelect={async () => {
                        await logout()
                        navigate('/problems')
                      }}
                    >
                      <LogOut /> Sign out
                    </DropdownMenuItem>
                  </>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </header>
      <main className="flex flex-1 flex-col">
        <Outlet />
      </main>
    </div>
  )
}
