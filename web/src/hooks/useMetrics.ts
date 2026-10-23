import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"

export interface MetricsByModelRow {
  model: string
  tasks_total: number
  tokens_in: number
  tokens_out: number
  cost_usd: number
}

export interface MetricsByDayRow {
  date: string
  tokens_in: number
  tokens_out: number
  cost_usd: number
  tasks_total: number
}

export type UsagePeriod = "today" | "24h" | "7d" | "30d" | "all"

/** One backend's share of one day: what the Usage chart stacks. */
export interface MetricsBackendDay {
  date: string
  backend: string
  tokens: number
  dispatches: number
  cost_usd: number
}

/** One dispatch in the Usage page's latest list. */
export interface MetricsRecent {
  ts: string
  task_id: string
  backend: string
  model: string
  tokens_in: number
  tokens_out: number
  cost_usd: number
  /** Priced from pricing.yaml or inferred, rather than reported by the CLI. */
  estimated: boolean
}

export interface MetricsResponse {
  project_id: string
  total_cost_usd: number
  /** The part of total_cost_usd priced from pricing.yaml, not reported by a CLI. */
  estimated_cost_usd: number
  by_model: MetricsByModelRow[]
  by_day: MetricsByDayRow[]
  estimate_hours_total: number
  period: UsagePeriod
  dispatches: number
  tokens_in: number
  cache_read_tokens: number
  cache_creation_tokens: number
  tokens_out: number
  by_backend_day: MetricsBackendDay[]
  recent: MetricsRecent[]
  /** Per by_model row: the provider that ran it and whether its cost is priced from pricing.yaml. */
  model_sources?: { model: string; backend: string; estimated: boolean }[]
}

async function fetchMetrics(period: UsagePeriod): Promise<MetricsResponse> {
  const { data } = await apiClient.get<MetricsResponse>("/api/metrics", { params: { period } })
  return data
}

/**
 * Fetches `/api/metrics` (operator profile). Cache key `['metrics']` is used
 * so the SSE-driven page can call `queryClient.invalidateQueries({queryKey:
 * ['metrics']})` when new events arrive during a run.
 */
export function useMetrics(period: UsagePeriod = "all") {
  return useQuery<MetricsResponse, Error>({
    queryKey: ["metrics", period],
    queryFn: () => fetchMetrics(period),
    staleTime: 30_000,
    refetchOnWindowFocus: false,
    retry: (failureCount, error) => {
      const status = (error as unknown as { response?: { status?: number } })
        ?.response?.status
      if (status === 401 || status === 403) return false
      return failureCount < 2
    },
  })
}
