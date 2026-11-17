import { Link } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowRight, Boxes, Code2, Cpu, Sparkles, Trophy } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { UserAvatar } from '@/components/brand'
import { DifficultyBars, ProgressRing } from '@/components/progress'
import { countByDifficulty } from '@/lib/progress'
import { useAllProblems } from '@/hooks/useProblemTitles'
import { useMyStanding } from '@/hooks/useStanding'
import { leaderboard } from '@/lib/endpoints'
import { displayNameOf } from '@/lib/format'
import { cn } from '@/lib/utils'

export default function LandingPage() {
  return (
    <div className="flex flex-col">
      <Hero />
      <div className="mx-auto grid w-full max-w-6xl gap-10 px-4 pb-16">
        <Stats />
        <YourProgress />
        <TopThree />
        <HowItWorks />
      </div>
    </div>
  )
}

function Hero() {
  return (
    <section className="relative overflow-hidden border-b bg-brand-glow">
      <div className="pointer-events-none absolute inset-0 bg-grid opacity-60" />
      <div className="relative mx-auto grid max-w-6xl items-center gap-12 px-4 py-16 md:py-24 lg:grid-cols-2">
        <div className="animate-in fade-in-0 slide-in-from-bottom-3 duration-500">
          <span className="inline-flex items-center gap-2 rounded-full border bg-background/60 px-3 py-1 text-xs text-muted-foreground backdrop-blur">
            <span className="size-1.5 rounded-full bg-success" /> Sandboxed judging · live verdicts
          </span>
          <h1 className="mt-5 text-4xl font-bold tracking-tight text-balance md:text-5xl">
            Practice problems. <span className="text-brand">Get judged</span> in seconds.
          </h1>
          <p className="mt-4 max-w-xl text-lg text-pretty text-muted-foreground">
            Write a solution in the browser, run it on the samples, then submit. Hidden tests run in an
            isolated sandbox, the verdict streams back live, and the leaderboard follows.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Button size="lg" asChild>
              <Link to="/problems">
                Start solving <ArrowRight />
              </Link>
            </Button>
            <Button size="lg" variant="outline" asChild>
              <Link to="/leaderboard">
                <Trophy /> Leaderboard
              </Link>
            </Button>
          </div>
        </div>
        <EditorPreview />
      </div>
    </section>
  )
}

/** A static picture of the workspace: code on top, an accepted result below. */
function EditorPreview() {
  return (
    <div className="animate-in fade-in-0 slide-in-from-bottom-4 rounded-xl border bg-card shadow-2xl shadow-primary/10 duration-700">
      <div className="flex items-center gap-2 border-b px-4 py-2.5">
        <span className="size-2.5 rounded-full bg-hard/70" />
        <span className="size-2.5 rounded-full bg-medium/70" />
        <span className="size-2.5 rounded-full bg-easy/70" />
        <span className="ml-3 font-mono text-xs text-muted-foreground">solution.py · Two Sum</span>
      </div>
      <pre className="overflow-x-auto p-4 font-mono text-[13px] leading-6">
        <code>
          <span className="text-brand">class</span> <span className="text-medium">Solution</span>:{'\n'}
          {'    '}
          <span className="text-brand">def</span> <span className="text-easy">twoSum</span>(self, nums,
          target):{'\n'}
          {'        '}seen = {'{}'}
          {'\n'}
          {'        '}
          <span className="text-brand">for</span> j, x <span className="text-brand">in</span> enumerate(nums):
          {'\n'}
          {'            '}
          <span className="text-brand">if</span> target - x <span className="text-brand">in</span> seen:{'\n'}
          {'                '}
          <span className="text-brand">return</span> [seen[target - x], j]{'\n'}
          {'            '}seen[x] = j
        </code>
      </pre>
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 border-t bg-success/8 px-4 py-3">
        <span className="font-semibold text-success">Accepted</span>
        <span className="text-sm text-muted-foreground">20 / 20 test cases passed</span>
        <span className="ml-auto font-mono text-xs text-muted-foreground">73 ms · 41.2 MB</span>
      </div>
    </div>
  )
}

function Stats() {
  const all = useAllProblems()
  const top = useQuery({ queryKey: ['leaderboard'], queryFn: () => leaderboard.top(100) })
  const count = (d: string) => all.data?.filter((p) => p.difficulty === d).length
  const items = [
    { label: 'Problems', value: all.data?.length, className: 'text-foreground' },
    { label: 'Easy', value: count('easy'), className: 'text-easy' },
    { label: 'Medium', value: count('medium'), className: 'text-medium' },
    { label: 'Hard', value: count('hard'), className: 'text-hard' },
    { label: 'Ranked coders', value: top.data?.entries.length, className: 'text-brand' },
  ]
  return (
    <section className="-mt-8 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
      {items.map((it) => (
        <div key={it.label} className="rounded-xl border bg-card p-4 shadow-sm">
          <div className="text-xs text-muted-foreground">{it.label}</div>
          {it.value === undefined ? (
            <Skeleton className="mt-2 h-7 w-12" />
          ) : (
            <div className={cn('mt-1 text-2xl font-semibold tabular-nums', it.className)}>{it.value}</div>
          )}
        </div>
      ))}
    </section>
  )
}

function YourProgress() {
  const all = useAllProblems()
  const standing = useMyStanding()
  if (!standing.signedIn || !all.data) return null
  const counts = countByDifficulty(all.data, standing.solved)
  return (
    <section className="rounded-xl border bg-card p-6 shadow-sm">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold">Your progress</h2>
        <span className="text-sm text-muted-foreground">
          {standing.data?.rank
            ? `Rank #${standing.data.rank} · ${standing.data.score} points`
            : 'Not ranked yet'}
        </span>
      </div>
      <div className="flex flex-col items-center gap-8 sm:flex-row">
        <ProgressRing solved={standing.solved.size} total={all.data.length} />
        <DifficultyBars counts={counts} className="w-full flex-1" />
      </div>
    </section>
  )
}

const PODIUM = [
  { place: 2, height: 'h-20', ring: 'ring-slate-300', label: 'bg-slate-300/20 text-slate-400' },
  { place: 1, height: 'h-28', ring: 'ring-amber-400', label: 'bg-amber-400/20 text-amber-500' },
  { place: 3, height: 'h-14', ring: 'ring-orange-400', label: 'bg-orange-400/20 text-orange-500' },
]

function TopThree() {
  const top = useQuery({ queryKey: ['leaderboard'], queryFn: () => leaderboard.top(100) })
  const entries = top.data?.entries ?? []
  if (entries.length === 0) return null
  return (
    <section className="rounded-xl border bg-card p-6 shadow-sm">
      <div className="mb-6 flex items-center justify-between">
        <h2 className="text-lg font-semibold">Top coders</h2>
        <Button variant="ghost" size="sm" asChild>
          <Link to="/leaderboard">
            Full leaderboard <ArrowRight />
          </Link>
        </Button>
      </div>
      <div className="flex items-end justify-center gap-4 sm:gap-8">
        {PODIUM.map(({ place, height, ring, label }) => {
          const e = entries[place - 1]
          if (!e) return <div key={place} className="w-24" />
          return (
            <div key={place} className="flex w-24 flex-col items-center gap-2 sm:w-32">
              <UserAvatar
                name={displayNameOf(e)}
                className={cn('size-12 text-sm ring-2 ring-offset-2 ring-offset-card', ring)}
              />
              <div className="w-full truncate text-center text-sm font-medium">{displayNameOf(e)}</div>
              <div className="text-xs text-muted-foreground tabular-nums">{e.score} pts</div>
              <div
                className={cn('grid w-full place-items-center rounded-t-lg text-lg font-bold', height, label)}
              >
                {place}
              </div>
            </div>
          )
        })}
      </div>
    </section>
  )
}

const STEPS = [
  {
    Icon: Code2,
    title: 'Write',
    text: 'Implement the function in a full editor, with the starter code and samples beside it.',
  },
  {
    Icon: Boxes,
    title: 'Judge',
    text: 'Each submission runs in a network-less sandbox on a worker pool, one test case at a time.',
  },
  {
    Icon: Cpu,
    title: 'Fair limits',
    text: 'CPU-time and memory limits per case: a brute force on the large hidden cases runs out of time.',
  },
  {
    Icon: Sparkles,
    title: 'Learn',
    text: 'Failed? AI hints explain what went wrong, but only the tests ever decide a verdict.',
  },
]

function HowItWorks() {
  return (
    <section>
      <h2 className="mb-4 text-lg font-semibold">How it works</h2>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {STEPS.map(({ Icon, title, text }) => (
          <div
            key={title}
            className="group rounded-xl border bg-card p-5 shadow-sm transition-colors hover:border-primary/40"
          >
            <span className="grid size-9 place-items-center rounded-lg bg-brand-soft text-brand transition-transform group-hover:scale-110">
              <Icon className="size-4.5" />
            </span>
            <h3 className="mt-3 font-semibold">{title}</h3>
            <p className="mt-1 text-sm text-muted-foreground">{text}</p>
          </div>
        ))}
      </div>
    </section>
  )
}
