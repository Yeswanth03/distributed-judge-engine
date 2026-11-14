import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ErrorState, Loading, PageContainer } from '@/components/states'
import { errorMessage } from '@/lib/errors'
import { useAuth } from '@/auth/useAuth'
import { users } from '@/lib/endpoints'
import { formatRelative } from '@/lib/format'

export default function ProfilePage() {
  const { state, reloadUser } = useAuth()
  const user = state.status === 'signedIn' ? state.user : null
  const [name, setName] = useState(user?.display_name ?? '')

  const save = useMutation({
    mutationFn: () => users.updateMe(name),
    onSuccess: async () => {
      await reloadUser()
      toast.success('Display name saved.')
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  if (!user) return null
  return (
    <PageContainer>
      <h1 className="mb-4 text-2xl font-semibold">Profile</h1>
      <div className="grid gap-4">
        <Card>
          <CardHeader>
            <CardTitle>Account</CardTitle>
            <CardDescription>
              {user.username} · {user.email} · {user.role}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form
              className="flex items-end gap-2"
              onSubmit={(e: FormEvent) => {
                e.preventDefault()
                save.mutate()
              }}
            >
              <div className="grid flex-1 gap-2">
                <Label htmlFor="display_name">Display name</Label>
                <Input id="display_name" value={name} onChange={(e) => setName(e.target.value)} />
              </div>
              <Button type="submit" disabled={save.isPending || name === user.display_name}>
                Save
              </Button>
            </form>
          </CardContent>
        </Card>
        <Sessions />
      </div>
    </PageContainer>
  )
}

function Sessions() {
  const queryClient = useQueryClient()
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: users.sessions })
  const revoke = useMutation({
    mutationFn: users.revokeSession,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['sessions'] }),
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Signed-in devices</CardTitle>
        <CardDescription>
          Signing a device out takes effect within 10 minutes, when its access token expires.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {sessions.isPending ? (
          <Loading />
        ) : sessions.isError ? (
          <ErrorState error={sessions.error} />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Device</TableHead>
                <TableHead>Address</TableHead>
                <TableHead>Last active</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {sessions.data.items.map((s) => (
                <TableRow key={s.session_id}>
                  <TableCell className="max-w-64 truncate" title={s.user_agent}>
                    {s.user_agent || 'Unknown device'}
                    {s.current && <span className="ml-2 text-xs text-muted-foreground">(this device)</span>}
                  </TableCell>
                  <TableCell>{s.ip}</TableCell>
                  <TableCell>{formatRelative(s.last_used_at)}</TableCell>
                  <TableCell className="text-right">
                    {!s.current && (
                      <Button variant="outline" size="sm" onClick={() => revoke.mutate(s.session_id)}>
                        Sign out
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}
