import { Code2 } from 'lucide-react'
import { cn } from '@/lib/utils'

export function BrandMark({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        'grid size-8 place-items-center rounded-lg bg-gradient-to-br from-primary to-[oklch(0.55_0.2_255)] text-primary-foreground shadow-sm shadow-primary/30',
        className,
      )}
    >
      <Code2 className="size-4.5" strokeWidth={2.5} />
    </span>
  )
}

export function Brand({ className }: { className?: string }) {
  return (
    <span className={cn('flex items-center gap-2.5 font-semibold tracking-tight', className)}>
      <BrandMark />
      <span>
        Mini Code <span className="text-brand">Judge</span>
      </span>
    </span>
  )
}

/** Initials in a coloured circle; the colour is stable per name. */
export function UserAvatar({ name, className }: { name: string; className?: string }) {
  const initials =
    name
      .split(/[\s_.-]+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((w) => w[0]!.toUpperCase())
      .join('') || '?'
  let hash = 0
  for (const ch of name) hash = (hash * 31 + ch.charCodeAt(0)) % 360
  return (
    <span
      className={cn(
        'grid size-8 shrink-0 place-items-center rounded-full text-xs font-semibold text-white',
        className,
      )}
      style={{ background: `oklch(0.6 0.14 ${hash})` }}
      aria-hidden="true"
    >
      {initials}
    </span>
  )
}
