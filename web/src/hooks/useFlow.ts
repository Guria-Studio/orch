import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"
import type { WelcomeState } from "@/pages/WelcomePage"

/**
 * `GET /api/flow` (#548): the welcome page's plan and apply steps for this
 * project, plus the run the Now page starts — internal/cli/welcome.go's
 * welcomeState, with `running` (an `orch run` holds this project) and
 * `run_error` (how the last run the dashboard started failed).
 */
export type FlowState = WelcomeState & { running: boolean; run_error?: string }

/** Polls while claude plans or a run the dashboard started is going. */
export function useFlow() {
  return useQuery({
    queryKey: ["flow"],
    queryFn: async () => (await apiClient.get<FlowState>("/api/flow")).data,
    refetchInterval: (q) => (q.state.data?.planning || q.state.data?.running ? 2000 : false),
    retry: false,
  })
}

/** One step — plan, apply or run; a refusal rejects with the server's `detail`. */
export async function flowStep(step: "plan" | "apply" | "run", body?: object): Promise<FlowState> {
  try {
    const { data } = await apiClient.post<FlowState>(`/api/flow/${step}`, body)
    return data
  } catch (err) {
    const detail = (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail
    throw new Error(detail ?? (err instanceof Error ? err.message : step))
  }
}
