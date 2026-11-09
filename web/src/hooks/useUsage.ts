import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"

/** One set of dispatches' tokens (internal/usage Tokens). */
export interface UsageTokens {
  dispatches: number
  input: number
  cached_input: number
  cache_write: number
  /** Includes reasoning. */
  output: number
  reasoning: number
  cost_usd: number
  estimated: boolean
}

export interface UsageTask extends UsageTokens {
  task_id: string
  attempts: (UsageTokens & { attempt: number })[]
  /** What attempts after the first used. */
  retry_tokens: number
}

/** `GET /api/usage` (#417): `orch report usage --json`, all time; heaviest first. */
export interface Usage {
  total: UsageTokens
  retry_tokens: number
  by_model: (UsageTokens & { backend: string; model: string })[]
  by_task: UsageTask[]
}

export function useUsage() {
  return useQuery({
    queryKey: ["metrics", "usage"],
    queryFn: async () => (await apiClient.get<Usage>("/api/usage")).data,
    staleTime: 30_000,
  })
}
