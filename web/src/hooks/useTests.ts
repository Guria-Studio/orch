import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"
import type { Evidence } from "@/lib/evidence"
import { runKey, type E2EReport, type E2ERun, type TestsResponse } from "@/lib/tests"

async function fetchTests(): Promise<TestsResponse> {
  const { data } = await apiClient.get<TestsResponse>("/api/tests")
  return data
}

// /api/tests (operator only): every E2E run the local verify collected. A new
// one arrives as an e2e_report event, which the event stream turns into a
// refetch.
export function useTestRuns() {
  return useQuery<TestsResponse, Error>({
    queryKey: ["tests"],
    queryFn: fetchTests,
  })
}

// One run's report. It never changes once written, so it is fetched once.
export function useTestReport(run: E2ERun | undefined) {
  return useQuery<E2EReport, Error>({
    queryKey: ["tests", "report", run ? runKey(run) : ""],
    queryFn: async () => {
      const { data } = await apiClient.get<E2EReport>(`/api/tests/${runKey(run!)}`)
      return data
    },
    enabled: run !== undefined,
    staleTime: Infinity,
  })
}

// A task's evidence (#520), null when it has none (404). Under the tests key,
// so a new E2E report or verify refetches it.
export function useTaskEvidence(taskId: string) {
  return useQuery<Evidence | null, Error>({
    queryKey: ["tests", "evidence", taskId],
    queryFn: async () => {
      try {
        const { data } = await apiClient.get<Evidence>(`/api/tests/evidence/${encodeURIComponent(taskId)}`)
        // A manifest this page cannot read is no evidence, not a broken page.
        return data?.version === 1 && Array.isArray(data.checks) ? data : null
      } catch (err) {
        if ((err as { response?: { status?: number } })?.response?.status === 404) return null
        throw err
      }
    },
    enabled: taskId !== "",
    retry: false,
  })
}
