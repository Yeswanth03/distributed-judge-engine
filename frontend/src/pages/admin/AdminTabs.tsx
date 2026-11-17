import { NavLink } from 'react-router'
import { cn } from '@/lib/utils'

const tab = ({ isActive }: { isActive: boolean }) =>
  cn(
    'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
    isActive ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground',
  )

export function AdminTabs() {
  return (
    <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Admin</h1>
        <p className="text-sm text-muted-foreground">Manage problems, test sets and accounts.</p>
      </div>
      <nav className="flex gap-1 rounded-lg border bg-muted/50 p-1">
        <NavLink to="/admin/problems" className={tab}>
          Problems
        </NavLink>
        <NavLink to="/admin/users" className={tab}>
          Users
        </NavLink>
      </nav>
    </div>
  )
}
