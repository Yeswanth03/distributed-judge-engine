import { api, COOKIE_TRANSPORT } from './api'
import type {
  AdminProblemSummary,
  AdminProblemView,
  CaseInput,
  Difficulty,
  Feedback,
  Leaderboard,
  Page,
  ProblemInput,
  ProblemSummary,
  ProblemView,
  Run,
  Session,
  Submission,
  SubmissionCreated,
  TestsetVersion,
  TokenResponse,
  User,
  UserStanding,
} from './types'

function query(params: Record<string, string | number | undefined | null>) {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') q.set(k, String(v))
  }
  const s = q.toString()
  return s ? `?${s}` : ''
}

// ---- User Service ----

export const auth = {
  login: (email: string, password: string) =>
    api<TokenResponse>('/auth/login', {
      method: 'POST',
      body: { email, password },
      headers: COOKIE_TRANSPORT,
    }),
  register: (body: { email: string; username: string; password: string; display_name?: string }) =>
    api<TokenResponse>('/auth/register', { method: 'POST', body, headers: COOKIE_TRANSPORT }),
  logout: () => api<void>('/auth/logout', { method: 'POST', body: {}, headers: COOKIE_TRANSPORT }),
  providers: () => api<{ providers: string[] }>('/auth/oauth/providers'),
  exchange: (login_code: string) =>
    api<TokenResponse>('/auth/oauth/exchange', {
      method: 'POST',
      body: { login_code },
      headers: COOKIE_TRANSPORT,
    }),
}

export const users = {
  me: () => api<User>('/users/me'),
  updateMe: (display_name: string) => api<User>('/users/me', { method: 'PATCH', body: { display_name } }),
  sessions: () => api<{ items: Session[] }>('/users/me/sessions'),
  revokeSession: (id: string) => api<void>(`/users/me/sessions/${id}`, { method: 'DELETE' }),
  list: (cursor?: string) => api<Page<User>>(`/users${query({ limit: 50, cursor })}`),
  setRole: (id: string, role: User['role']) =>
    api<User>(`/users/${id}/role`, { method: 'PATCH', body: { role } }),
  setStatus: (id: string, status: User['status']) =>
    api<User>(`/users/${id}/status`, { method: 'PATCH', body: { status } }),
}

// ---- Catalog ----

export const problems = {
  list: (difficulty?: Difficulty, cursor?: string) =>
    api<Page<ProblemSummary>>(`/problems${query({ difficulty, cursor, limit: 50 })}`),
  get: (slug: string) => api<ProblemView>(`/problems/${encodeURIComponent(slug)}`),
}

export const leaderboard = {
  top: (limit = 50) => api<Leaderboard>(`/leaderboard${query({ limit })}`),
  standing: (userId: string) => api<UserStanding>(`/leaderboard/users/${encodeURIComponent(userId)}`),
}

export const admin = {
  list: (status?: 'draft' | 'published', cursor?: string) =>
    api<Page<AdminProblemSummary>>(`/admin/problems${query({ status, cursor, limit: 50 })}`),
  get: (id: string) => api<AdminProblemView>(`/admin/problems/${id}`),
  create: (body: ProblemInput & { slug: string }) =>
    api<{ id: string }>('/admin/problems', { method: 'POST', body }),
  update: (id: string, body: ProblemInput) => api<void>(`/admin/problems/${id}`, { method: 'PUT', body }),
  versions: (id: string) => api<TestsetVersion[]>(`/admin/problems/${id}/testsets`),
  upload: (id: string, cases: CaseInput[]) =>
    api<{ version: number; content_hash: string }>(`/admin/problems/${id}/testsets`, {
      method: 'POST',
      body: { cases },
    }),
  publish: (id: string, version: number) =>
    api<void>(`/admin/problems/${id}/testsets/${version}/publish`, { method: 'POST' }),
}

// ---- Submission Service ----

export const submissions = {
  create: (problem_id: string, code: string) =>
    api<SubmissionCreated>('/api/v1/submissions', {
      method: 'POST',
      body: { problem_id, language: 'python', code },
    }),
  get: (id: string, includeCode = false) =>
    api<Submission>(`/api/v1/submissions/${id}${includeCode ? '?include=code' : ''}`),
  list: (problemId?: string, cursor?: string) =>
    api<Page<Submission>>(`/api/v1/submissions${query({ problem_id: problemId, cursor, limit: 20 })}`),
}

export const runs = {
  create: (problem_id: string, code: string, inputs?: string[]) =>
    api<{ run_id: string }>('/api/v1/runs', {
      method: 'POST',
      body: { problem_id, language: 'python', code, ...(inputs ? { inputs } : {}) },
    }),
  get: (id: string) => api<Run>(`/api/v1/runs/${id}`),
}

// ---- AI Service (the gateway strips /api/v1/ai) ----

export const feedback = {
  request: (submissionId: string) =>
    api<Feedback>(`/api/v1/ai/submissions/${submissionId}/feedback`, { method: 'POST' }),
  get: (submissionId: string) => api<Feedback>(`/api/v1/ai/submissions/${submissionId}/feedback`),
}
