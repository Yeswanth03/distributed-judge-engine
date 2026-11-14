import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ErrorState, Loading, PageContainer } from '@/components/states'
import { errorMessage } from '@/lib/errors'
import { useAuth } from '@/auth/useAuth'
import { users } from '@/lib/endpoints'
import { formatRelative } from '@/lib/format'
import type { User } from '@/lib/types'
import { AdminTabs } from './AdminTabs'

export default function AdminUsersPage() {
  const { state } = useAuth()
  const meId = state.status === 'signedIn' ? state.user.user_id : null
  const queryClient = useQueryClient()

  const list = useInfiniteQuery({
    queryKey: ['admin-users'],
    queryFn: ({ pageParam }) => users.list(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })

  const change = useMutation({
    mutationFn: ({ id, role, status }: { id: string; role?: User['role']; status?: User['status'] }) =>
      role ? users.setRole(id, role) : users.setStatus(id, status!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin-users'] })
      toast.success('Saved. It takes effect within 10 minutes, when their access token renews.')
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <PageContainer wide>
      <AdminTabs />
      {list.isPending ? (
        <Loading />
      ) : list.isError ? (
        <ErrorState error={list.error} />
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>User</TableHead>
                <TableHead>Email</TableHead>
                <TableHead className="w-36">Role</TableHead>
                <TableHead className="w-36">Status</TableHead>
                <TableHead className="text-right">Joined</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.data.pages
                .flatMap((p) => p.items)
                .map((u) => {
                  const self = u.user_id === meId // the User Service refuses changes to your own role or status
                  return (
                    <TableRow key={u.user_id}>
                      <TableCell>
                        <div className="font-medium">{u.display_name || u.username}</div>
                        <div className="text-xs text-muted-foreground">@{u.username}</div>
                      </TableCell>
                      <TableCell>{u.email}</TableCell>
                      <TableCell>
                        <Select
                          value={u.role}
                          disabled={self}
                          onValueChange={(role) =>
                            change.mutate({ id: u.user_id, role: role as User['role'] })
                          }
                        >
                          <SelectTrigger aria-label={`Role of ${u.username}`}>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="user">User</SelectItem>
                            <SelectItem value="admin">Admin</SelectItem>
                          </SelectContent>
                        </Select>
                      </TableCell>
                      <TableCell>
                        <Select
                          value={u.status}
                          disabled={self}
                          onValueChange={(status) =>
                            change.mutate({ id: u.user_id, status: status as User['status'] })
                          }
                        >
                          <SelectTrigger aria-label={`Status of ${u.username}`}>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="active">Active</SelectItem>
                            <SelectItem value="disabled">Disabled</SelectItem>
                          </SelectContent>
                        </Select>
                      </TableCell>
                      <TableCell className="text-right text-muted-foreground">
                        {formatRelative(u.created_at)}
                      </TableCell>
                    </TableRow>
                  )
                })}
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
