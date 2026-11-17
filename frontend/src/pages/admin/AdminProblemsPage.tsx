import { useInfiniteQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router'
import { Plus } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { DifficultyLabel } from '@/components/badges'
import { ErrorState, Loading, PageContainer } from '@/components/states'
import { admin } from '@/lib/endpoints'
import { formatRelative } from '@/lib/format'
import { AdminTabs } from './AdminTabs'

export default function AdminProblemsPage() {
  const [params, setParams] = useSearchParams()
  const status = (params.get('status') as 'draft' | 'published' | null) ?? undefined

  const list = useInfiniteQuery({
    queryKey: ['admin-problems', status],
    queryFn: ({ pageParam }) => admin.list(status, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })

  return (
    <PageContainer wide>
      <AdminTabs />
      <div className="mb-4 flex items-center gap-3">
        <Select value={status ?? 'all'} onValueChange={(v) => setParams(v === 'all' ? {} : { status: v })}>
          <SelectTrigger className="w-40" aria-label="Status">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All statuses</SelectItem>
            <SelectItem value="draft">Drafts</SelectItem>
            <SelectItem value="published">Published</SelectItem>
          </SelectContent>
        </Select>
        <Button className="ml-auto" asChild>
          <Link to="/admin/problems/new">
            <Plus /> New problem
          </Link>
        </Button>
      </div>
      {list.isPending ? (
        <Loading />
      ) : list.isError ? (
        <ErrorState error={list.error} />
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Title</TableHead>
                <TableHead>Slug</TableHead>
                <TableHead>Difficulty</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Test set</TableHead>
                <TableHead className="text-right">Updated</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.data.pages
                .flatMap((p) => p.items)
                .map((p) => (
                  <TableRow key={p.id}>
                    <TableCell>
                      <Link to={`/admin/problems/${p.id}`} className="font-medium hover:underline">
                        {p.title}
                      </Link>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{p.slug}</TableCell>
                    <TableCell>
                      <DifficultyLabel difficulty={p.difficulty} />
                    </TableCell>
                    <TableCell>
                      <Badge variant={p.status === 'published' ? 'default' : 'secondary'}>{p.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {p.current_version !== null ? `v${p.current_version}` : '—'}
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground">
                      {formatRelative(p.updated_at)}
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
          {list.hasNextPage && (
            <div className="mt-4 flex justify-center">
              <Button
                variant="outline"
                onClick={() => list.fetchNextPage()}
                disabled={list.isFetchingNextPage}
              >
                Load more
              </Button>
            </div>
          )}
        </>
      )}
    </PageContainer>
  )
}
