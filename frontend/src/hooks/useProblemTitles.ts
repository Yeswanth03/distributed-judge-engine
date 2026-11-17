import { useQuery } from '@tanstack/react-query'
import { problems } from '@/lib/endpoints'

/** Problem titles by id, from the public list (submissions only carry the problem id). */
export function useProblemTitles() {
  const { data } = useQuery({
    queryKey: ['problem-titles'],
    queryFn: async () => {
      const titles = new Map<string, { title: string; slug: string }>()
      let cursor: string | undefined
      do {
        const page = await problems.list(undefined, cursor)
        page.items.forEach((p) => titles.set(p.id, { title: p.title, slug: p.slug }))
        cursor = page.next_cursor ?? undefined
      } while (cursor)
      return titles
    },
    staleTime: 5 * 60_000,
  })
  return data
}
