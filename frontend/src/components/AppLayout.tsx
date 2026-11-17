import { useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router'
import { LogOut, Menu, Monitor, Moon, Shield, Sun, User as UserIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from '@/components/ui/sheet'
import { Brand, UserAvatar } from '@/components/brand'
import { useAuth } from '@/auth/useAuth'
import { useTheme, type Theme } from '@/theme/theme'
import { cn } from '@/lib/utils'

const navClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
    isActive
      ? 'bg-accent text-accent-foreground'
      : 'text-muted-foreground hover:bg-muted hover:text-foreground',
  )

const THEMES: { value: Theme; label: string; Icon: typeof Sun }[] = [
  { value: 'light', label: 'Light', Icon: Sun },
  { value: 'dark', label: 'Dark', Icon: Moon },
  { value: 'system', label: 'System', Icon: Monitor },
]

function ThemeSwitch() {
  const { theme, setTheme } = useTheme()
  return (
    <div
      className="flex items-center rounded-lg border bg-muted/50 p-0.5"
      role="radiogroup"
      aria-label="Theme"
    >
      {THEMES.map(({ value, label, Icon }) => (
        <button
          key={value}
          type="button"
          role="radio"
          aria-checked={theme === value}
          aria-label={label}
          title={label}
          onClick={() => setTheme(value)}
          className={cn(
            'grid size-7 place-items-center rounded-md transition-colors',
            theme === value
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          <Icon className="size-3.5" />
        </button>
      ))}
    </div>
  )
}

export function AppLayout() {
  const { state, logout } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [menuOpen, setMenuOpen] = useState(false)
  const user = state.status === 'signedIn' ? state.user : null
  const name = user ? user.display_name || user.username : ''
  // The problem workspace uses the full width and height; other pages get a footer.
  const workspace = /^\/problems\/[^/]+$/.test(location.pathname)

  const links = (
    <>
      <NavLink to="/problems" className={navClass} onClick={() => setMenuOpen(false)}>
        Problems
      </NavLink>
      <NavLink to="/leaderboard" className={navClass} onClick={() => setMenuOpen(false)}>
        Leaderboard
      </NavLink>
      {user && (
        <NavLink to="/submissions" className={navClass} onClick={() => setMenuOpen(false)}>
          Submissions
        </NavLink>
      )}
      {user?.role === 'admin' && (
        <NavLink to="/admin" className={navClass} onClick={() => setMenuOpen(false)}>
          Admin
        </NavLink>
      )}
    </>
  )

  return (
    <div className="flex min-h-svh flex-col">
      <header className="sticky top-0 z-40 border-b bg-background/80 backdrop-blur-md supports-[backdrop-filter]:bg-background/70">
        <div className="mx-auto flex h-14 w-full max-w-screen-2xl items-center gap-4 px-4">
          <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
            <SheetTrigger asChild>
              <Button variant="ghost" size="icon" className="md:hidden" aria-label="Open menu">
                <Menu />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="w-72">
              <SheetHeader>
                <SheetTitle>
                  <Brand />
                </SheetTitle>
              </SheetHeader>
              <nav className="flex flex-col gap-1 px-4">{links}</nav>
              <div className="mt-auto flex items-center justify-between border-t p-4">
                <span className="text-sm text-muted-foreground">Theme</span>
                <ThemeSwitch />
              </div>
            </SheetContent>
          </Sheet>

          <Link to="/" className="shrink-0">
            <Brand />
          </Link>
          <nav className="hidden items-center gap-1 md:flex">{links}</nav>

          <div className="ml-auto flex items-center gap-2">
            <div className="hidden sm:block">
              <ThemeSwitch />
            </div>
            {state.status === 'signedOut' && (
              <>
                <Button variant="ghost" size="sm" asChild>
                  <Link to="/login">Sign in</Link>
                </Button>
                <Button size="sm" asChild>
                  <Link to="/register">Sign up</Link>
                </Button>
              </>
            )}
            {user && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <button
                    type="button"
                    aria-label="Account menu"
                    className="rounded-full ring-offset-background transition-shadow hover:ring-2 hover:ring-ring/40 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                  >
                    <UserAvatar name={name} />
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-60">
                  <DropdownMenuLabel className="flex items-center gap-3">
                    <UserAvatar name={name} className="size-9" />
                    <div className="min-w-0">
                      <div className="truncate font-medium">{name}</div>
                      <div className="truncate text-xs font-normal text-muted-foreground">{user.email}</div>
                    </div>
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
                  <DropdownMenuItem
                    onSelect={async () => {
                      await logout()
                      navigate('/')
                    }}
                  >
                    <LogOut /> Sign out
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>
        </div>
      </header>
      <main
        key={location.pathname}
        className="flex flex-1 flex-col animate-in fade-in-0 slide-in-from-bottom-1 duration-300"
      >
        <Outlet />
      </main>
      {!workspace && (
        <footer className="border-t py-6 text-center text-xs text-muted-foreground">
          Mini Code Judge · COMP41720 distributed systems project · Verdicts come only from sandboxed tests
        </footer>
      )}
    </div>
  )
}
