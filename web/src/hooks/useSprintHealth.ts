import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"
import type { SprintHealth } from "@/lib/types"

async function fetchSprintHealth(phase: string): Promise<SprintHealth> {
  const { data } = await apiClient.get<SprintHealth>("/api/sprint", { params: phase ? { phase } : undefined })
  return data
}

/** The project's pace, or one phase's with `phase` ("" for all). */
export function useSprintHealth(phase = "") {
  return useQuery({
    queryKey: ["sprint-health", phase],
    queryFn: () => fetchSprintHealth(phase),
    refetchInterval: 30_000,
  })
}
