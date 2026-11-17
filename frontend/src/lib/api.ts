import type { TokenResponse } from './types'

/**
 * Every call goes to the gateway under /api (the dev proxy, and later the production server, strips the prefix),
 * which keeps API paths apart from page URLs such as /problems/two-sum.
 */
const PREFIX = '/api'

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

// ---- access token ----
// Kept in memory only, never in storage: a page script cannot read what was never written down. The refresh
// token lives in an HttpOnly cookie the browser sends to /auth/refresh by itself.

let accessToken: string | null = null
const listeners = new Set<(token: string | null) => void>()

export function getAccessToken() {
  return accessToken
}

export function setAccessToken(token: string | null) {
  accessToken = token
  listeners.forEach((l) => l(token))
}

export function onAccessTokenChange(listener: (token: string | null) => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/** Asks the User Service to keep the refresh token in its HttpOnly cookie instead of the response body. */
export const COOKIE_TRANSPORT = { 'X-Token-Transport': 'cookie' }

let refreshing: Promise<TokenResponse | null> | null = null

/**
 * Trades the refresh cookie for a new access token. Concurrent callers share one request: each refresh token works
 * once, so two parallel refreshes would make the server see a reuse and revoke the session.
 */
export function refreshSession(): Promise<TokenResponse | null> {
  refreshing ??= (async () => {
    try {
      const res = await fetch(`${PREFIX}/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...COOKIE_TRANSPORT },
        body: '{}',
      })
      if (!res.ok) {
        setAccessToken(null)
        return null
      }
      const tokens = (await res.json()) as TokenResponse
      setAccessToken(tokens.access_token)
      return tokens
    } catch {
      setAccessToken(null)
      return null
    } finally {
      refreshing = null
    }
  })()
  return refreshing
}

// ---- requests ----

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export interface RequestOptions {
  method?: Method
  body?: unknown
  headers?: Record<string, string>
  signal?: AbortSignal
}

/** A gateway path such as /problems or /api/v1/submissions, as the browser must request it. */
export function apiUrl(path: string) {
  return `${PREFIX}${path}`
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  let res = await send(path, options)
  if (res.status === 401 && accessToken !== null) {
    // The access token expired (they live 10 minutes): refresh once and repeat the request.
    if (await refreshSession()) {
      res = await send(path, options)
    }
  }
  if (!res.ok) {
    throw await toError(res)
  }
  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

function send(path: string, { method = 'GET', body, headers: extra, signal }: RequestOptions) {
  const headers: Record<string, string> = { ...extra }
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }
  if (accessToken) {
    headers.Authorization = `Bearer ${accessToken}`
  }
  return fetch(apiUrl(path), {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
}

/** Services answer {error, message}; the AI Service (FastAPI) answers {detail}. */
async function toError(res: Response): Promise<ApiError> {
  try {
    const data = (await res.json()) as { error?: string; message?: string; detail?: unknown }
    const message = data.message ?? (typeof data.detail === 'string' ? data.detail : undefined)
    return new ApiError(res.status, data.error ?? 'error', message ?? res.statusText)
  } catch {
    return new ApiError(res.status, 'error', res.statusText || `request failed (${res.status})`)
  }
}
