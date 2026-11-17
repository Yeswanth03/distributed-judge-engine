import { useQuery } from '@tanstack/react-query'
import { useAuth } from '@/auth/useAuth'
import { leaderboard } from '@/lib/endpoints'

/** The signed-in user's leaderboard standing; a user with no verdict yet answers 404, which reads as none. */
export function useMyStanding() {
  const { state } = useAuth()
  const userId = state.status === 'signedIn' ? state.user.user_id : null
  const query = useQuery({
    queryKey: ['standing', userId],
    queryFn: () => leaderboard.standing(userId!),
    enabled: userId !== null,
    retry: false,
  })
  return { ...query, signedIn: userId !== null, solved: new Set(query.data?.solved_problem_ids ?? []) }
}
