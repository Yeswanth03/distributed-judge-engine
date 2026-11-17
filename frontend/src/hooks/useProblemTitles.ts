import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { problems } from '@/lib/endpoints'
import type { ProblemSummary } from '@/lib/types'

/** Every published problem (all pages), for counts, progress and titles by id. */
export function useAllProblems() {
  return useQuery({
    queryKey: ['all-problems'],
    queryFn: async () => {
      const all: ProblemSummary[] = []
      let cursor: string | undefined
      do {
        const page = await problems.list(undefined, cursor)
        all.push(...page.items)
        cursor = page.next_cursor ?? undefined
      } while (cursor)
      return all
    },
    staleTime: 5 * 60_000,
  })
}

/** Problem titles by id (submissions only carry the problem id). */
export function useProblemTitles() {
  const { data } = useAllProblems()
  return useMemo(
    () => (data ? new Map(data.map((p) => [p.id, { title: p.title, slug: p.slug }])) : undefined),
    [data],
  )
}
