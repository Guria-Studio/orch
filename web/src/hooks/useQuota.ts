import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"

// GET /api/quota (internal/dashboard/quotaroutes.go): the subscription windows
// each provider last reported (claude's rate_limit_event).

export interface QuotaWindow {
  /** The provider's own name: "five_hour", "seven_day". */
  window: string
  /** Used fraction, 0–1. */
  utilization: number
  /** Unix seconds; 0 when not given. */
  resets_at: number
}

export interface ProviderQuota {
  backend: string
  windows: QuotaWindow[]
  /** The dispatch that carried the report: as fresh as the last run, no fresher; "" with no report. */
  seen_at: string
  /** Turned off by the operator (`orch provider disable`): its tasks wait. */
  disabled?: boolean
  reason?: string
}

export function useQuota() {
  return useQuery<{ providers: ProviderQuota[] }, Error>({
    queryKey: ["quota"],
    queryFn: async () => (await apiClient.get<{ providers: ProviderQuota[] }>("/api/quota")).data,
    staleTime: 30_000,
  })
}
