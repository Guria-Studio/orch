import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"
import type { UsagePeriod } from "@/hooks/useMetrics"

/** One route's record on one kind of task (`orch router suggest`, #457). */
export interface RouteRecord {
  route: string
  current: boolean
  tasks: number
  first_try_rate: number
  mean_attempts: number
  attempts: number
  mean_cost_usd: number
  mean_duration_s: number
}

export interface RouteSuggestion {
  key: string
  from: string
  to: string
  why: "cheaper" | "stronger"
  to_backend: string
  to_cli_model: string
  reason: string
  cost_share: number
  first_try_from: number
  first_try_to: number
}

export interface KindRoutes {
  kind: string
  current: string
  routes: RouteRecord[]
  suggestion: RouteSuggestion | null
}

export interface RouterSuggestResponse {
  period: UsagePeriod
  min_sample: number
  kinds: KindRoutes[]
}

/** Fetches `/api/router/suggest` for the Usage page's period (operator profile). */
export function useRouterSuggest(period: UsagePeriod) {
  return useQuery<RouterSuggestResponse, Error>({
    queryKey: ["metrics", "router-suggest", period],
    queryFn: async () => (await apiClient.get<RouterSuggestResponse>("/api/router/suggest", { params: { period } })).data,
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  })
}
