import type { RunCaseVerdict, Verdict } from './types'

const VERDICT_TEXT: Record<Verdict | RunCaseVerdict, string> = {
  ACCEPTED: 'Accepted',
  WRONG_ANSWER: 'Wrong Answer',
  TIME_LIMIT_EXCEEDED: 'Time Limit Exceeded',
  MEMORY_LIMIT_EXCEEDED: 'Memory Limit Exceeded',
  RUNTIME_ERROR: 'Runtime Error',
  COMPILATION_ERROR: 'Compilation Error',
  INTERNAL_ERROR: 'Internal Error',
  FINISHED: 'Finished',
  INVALID_INPUT: 'Invalid Input',
}

export function verdictText(v: Verdict | RunCaseVerdict) {
  return VERDICT_TEXT[v] ?? v
}
