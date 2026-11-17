import { NavLink } from 'react-router'
import { cn } from '@/lib/utils'

const tab = ({ isActive }: { isActive: boolean }) =>
  cn(
    'border-b-2 px-1 pb-2 text-sm transition-colors',
    isActive
      ? 'border-foreground font-medium text-foreground'
      : 'border-transparent text-muted-foreground hover:text-foreground',
  )

export function AdminTabs() {
  return (
    <nav className="mb-6 flex gap-6 border-b">
      <NavLink to="/admin/problems" className={tab}>
        Problems
      </NavLink>
      <NavLink to="/admin/users" className={tab}>
        Users
      </NavLink>
    </nav>
  )
}
