import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { fetchEventSource } from '@microsoft/fetch-event-source'
import { apiUrl, getAccessToken, refreshSession } from '@/lib/api'
import type { Submission, SubmissionStatus, Verdict } from '@/lib/types'

export type StreamState = Partial<Submission> & { status: SubmissionStatus; verdict?: Verdict | null }

class RetryableError extends Error {}
class FatalError extends Error {}

/**
 * Follows one submission (mount it keyed by the submission id) over Server-Sent Events. fetch is used instead of EventSource because EventSource can't
 * send the Authorization header the gateway checks. The server sends the current state first, then changes, and
 * closes after the verdict.
 */
export function useSubmissionStream(submissionId: string | null) {
  const [state, setState] = useState<StreamState | null>(submissionId ? { status: 'QUEUED' } : null)
  const [error, setError] = useState<string | null>(null)
  const queryClient = useQueryClient()

  useEffect(() => {
    if (!submissionId) return
    const controller = new AbortController()

    fetchEventSource(apiUrl(`/api/v1/submissions/${submissionId}/events`), {
      signal: controller.signal,
      openWhenHidden: true,
      // Read the token on each (re)connect, so a refreshed token is used after a reconnect.
      fetch: (input, init) =>
        fetch(input, {
          ...init,
          headers: { ...init?.headers, Authorization: `Bearer ${getAccessToken() ?? ''}` },
        }),
      async onopen(res) {
        if (res.ok) return
        if (res.status === 401 && (await refreshSession())) throw new RetryableError('token refreshed')
        throw new FatalError(`live updates failed (${res.status})`)
      },
      onmessage(ev) {
        if (!ev.data) return
        const data = JSON.parse(ev.data) as StreamState
        setState((prev) => ({ ...prev, ...data }))
        if (ev.event === 'verdict') {
          controller.abort()
          queryClient.invalidateQueries({ queryKey: ['submissions'] })
          queryClient.invalidateQueries({ queryKey: ['standing'] })
        }
      },
      onerror(err) {
        if (err instanceof FatalError) {
          setError(err.message)
          throw err // stop retrying
        }
        // Anything else (dropped connection, refreshed token): let the library reconnect.
      },
    }).catch(() => {})

    return () => controller.abort()
  }, [submissionId, queryClient])

  return { state, error }
}
