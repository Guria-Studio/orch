import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"
import type { CiResponse } from "@/lib/ci"

async function fetchCI(): Promise<CiResponse> {
  const { data } = await apiClient.get<CiResponse>("/api/ci")
  return data
}

// /api/ci (operator only): the pipeline as config.yaml and the repo's
// workflows set it up, and the merge queue, which moves during a run.
export function useCI() {
  return useQuery<CiResponse, Error>({
    queryKey: ["ci"],
    queryFn: fetchCI,
    staleTime: 15_000,
    // The merge queue on this page moves during a run.
    refetchInterval: 15_000,
  })
}
