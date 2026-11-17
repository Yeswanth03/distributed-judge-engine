import type { Difficulty, ProblemSummary } from './types'

export interface DifficultyCount {
  difficulty: Difficulty
  solved: number
  total: number
}

/** Solved / total per difficulty, from the problem list and the user's solved ids. */
export function countByDifficulty(problems: ProblemSummary[], solved: Set<string>): DifficultyCount[] {
  return (['easy', 'medium', 'hard'] as const).map((difficulty) => {
    const ofDifficulty = problems.filter((p) => p.difficulty === difficulty)
    return {
      difficulty,
      total: ofDifficulty.length,
      solved: ofDifficulty.filter((p) => solved.has(p.id)).length,
    }
  })
}
