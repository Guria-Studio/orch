import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"

// The Quota page's additions (internal/dashboard/budgetroutes.go): editing a
// provider's budget window — `orch budget set` — and model_router.yaml's
// routing table.

export interface BudgetLimits {
  token_budget?: number
  window_hours?: number
  threshold_pct?: number
}

/** POST /api/budget/{provider}; a refusal rejects with the server's reason. */
export async function setBudgetLimits(provider: string, limits: BudgetLimits): Promise<Required<BudgetLimits>> {
  try {
    const { data } = await apiClient.post<Required<BudgetLimits>>(`/api/budget/${encodeURIComponent(provider)}`, limits)
    return data
  } catch (err) {
    const detail = (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail
    throw new Error(detail ?? (err instanceof Error ? err.message : "could not save the limits"))
  }
}

export interface RouteRow {
  /** The `model` a task names in tasks.json. */
  key: string
  backend: string
  cli_model: string
  tier: string
  /** concurrency.per_provider for the backend, else global_max. */
  max_parallel: number
}

export interface RouterPayload {
  path: string
  global_max: number
  routes: RouteRow[]
  /** Why the table is empty: a router or config that does not load. */
  error?: string
}

export function useRouterTable() {
  return useQuery<RouterPayload, Error>({
    queryKey: ["router"],
    queryFn: async () => (await apiClient.get<RouterPayload>("/api/router")).data,
    staleTime: 60_000,
  })
}
