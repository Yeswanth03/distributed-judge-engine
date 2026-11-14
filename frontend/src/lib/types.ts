// Response and request shapes of the backend, as documented in docs/contracts.md. JSON is snake_case.

export type Difficulty = 'easy' | 'medium' | 'hard'
export type Role = 'user' | 'admin'

// ---- User Service ----

export interface User {
  user_id: string
  email: string
  username: string
  display_name: string
  role: Role
  status: 'active' | 'disabled'
  email_verified: boolean
  created_at: string
}

export interface TokenResponse {
  access_token: string
  token_type: 'Bearer'
  expires_in: number
  refresh_expires_in: number
  user?: User
}

export interface Session {
  session_id: string
  user_agent: string
  ip: string
  created_at: string
  last_used_at: string
  expires_at: string
  current: boolean
}

export interface Page<T> {
  items: T[]
  next_cursor: string | null
}

// ---- Catalog ----

export interface ProblemSummary {
  id: string
  slug: string
  title: string
  difficulty: Difficulty
  points: number
}

export interface Sample {
  input: string
  expected_output: string
}

export interface FunctionParameter {
  name: string
  type: string
}

export interface ProblemView extends ProblemSummary {
  statement_md: string
  time_limit_ms: number
  memory_limit_mb: number
  starter_code: string | null
  samples: Sample[]
}

export interface AdminProblemSummary extends ProblemSummary {
  status: 'draft' | 'published'
  current_version: number | null
  updated_at: string
}

export interface AdminProblemView extends Omit<ProblemView, 'samples'> {
  function_name: string | null
  parameters: FunctionParameter[] | null
  return_type: string | null
  status: 'draft' | 'published'
  current_version: number | null
  created_at: string
  updated_at: string
}

export interface ProblemInput {
  title: string
  statement_md: string
  difficulty: Difficulty
  points: number
  time_limit_ms: number
  memory_limit_mb: number
  function_name: string
  parameters: FunctionParameter[]
  return_type: string
  starter_code: string
}

export interface TestsetVersion {
  version: number
  status: 'draft' | 'published'
  current: boolean
  content_hash: string
  case_count: number
  sample_count: number
  created_at: string
  published_at: string | null
}

export interface CaseInput {
  input: string
  expected_output: string
  is_sample: boolean
}

export interface LeaderboardEntry {
  rank: number
  user_id: string
  // Filled once Catalog consumes the User Service's user events; absent or null until it has seen the user.
  username?: string | null
  display_name?: string | null
  score: number
  solved_count: number
}

export interface Leaderboard {
  as_of: string | null
  lag_seconds: number | null
  entries: LeaderboardEntry[]
}

export interface UserStanding {
  user_id: string
  rank: number | null
  score: number
  solved_count: number
  solved_problem_ids: string[]
  as_of: string | null
  lag_seconds: number | null
}

// ---- Submission Service ----

export type SubmissionStatus = 'QUEUED' | 'RUNNING' | 'COMPLETED'

export type Verdict =
  | 'ACCEPTED'
  | 'WRONG_ANSWER'
  | 'TIME_LIMIT_EXCEEDED'
  | 'MEMORY_LIMIT_EXCEEDED'
  | 'RUNTIME_ERROR'
  | 'COMPILATION_ERROR'
  | 'INTERNAL_ERROR'

export interface Submission {
  submission_id: string
  user_id: string
  problem_id: string
  language: string
  status: SubmissionStatus
  verdict: Verdict | null
  execution_time_ms: number | null
  memory_kb: number | null
  test_cases_passed: number | null
  total_test_cases: number | null
  error_message: string | null
  created_at: string
  updated_at: string
  code?: string
}

export interface SubmissionCreated {
  submission_id: string
  status: SubmissionStatus
  created_at: string
}

export type RunCaseVerdict =
  | 'ACCEPTED'
  | 'WRONG_ANSWER'
  | 'FINISHED'
  | 'TIME_LIMIT_EXCEEDED'
  | 'MEMORY_LIMIT_EXCEEDED'
  | 'RUNTIME_ERROR'
  | 'INVALID_INPUT'

export interface RunCase {
  kind: 'sample' | 'custom'
  input: string
  expected_output: string | null
  output: string | null
  stdout: string | null
  error: string | null
  verdict: RunCaseVerdict
  time_ms: number | null
  memory_kb: number | null
}

export interface Run {
  run_id: string
  problem_id: string
  language: string
  status: 'QUEUED' | 'RUNNING' | 'COMPLETED' | 'FAILED'
  error_message: string | null
  result: { verdict: Verdict | null; finished_at: string; cases: RunCase[] } | null
  created_at: string
  updated_at: string
}

// ---- AI Service ----

export interface Feedback {
  request_id: string
  submission_id: string
  problem_id: string | null
  verdict: string
  status: 'queued' | 'processing' | 'ready' | 'unavailable'
  feedback: string | null
  error_reason: string | null
  created_at: string
  updated_at: string
}
