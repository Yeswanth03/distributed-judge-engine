import { useEffect, useState } from 'react'

function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null // storage unavailable (private window, blocked site data)
  }
}

/**
 * The code being written, kept per problem in this browser so a reload doesn't lose it. The caller is keyed by
 * problem, so the saved draft is read once when it mounts. Every storage access is guarded; the editor works
 * without it.
 */
export function useCodeDraft(problemId: string, starter: string) {
  const key = `draft:${problemId}`
  const [code, setCode] = useState(() => read(key) ?? starter)

  useEffect(() => {
    const t = setTimeout(() => {
      try {
        localStorage.setItem(key, code)
      } catch {
        /* storage unavailable */
      }
    }, 400)
    return () => clearTimeout(t)
  }, [key, code])

  return [code, setCode] as const
}
