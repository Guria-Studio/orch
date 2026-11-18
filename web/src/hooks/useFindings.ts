import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"

// GET /api/findings (internal/dashboard/findingsroutes.go, #536): the reports
// this project's agents queued about orch, with the exact title and body
// sending would file.

export interface Finding {
  id: number
  status: "queued" | "sent" | "dismissed"
  title: string
  body: string
  task_id: string
  queued_at: string
  url: string
}

export interface FindingsPayload {
  findings: Finding[]
  pending: number
  can_send: boolean
  repo: string
  // is_local (#617): whether THIS request is the loopback operator write
  // gate would allow. Distinct from can_send (a Filer is configured at
  // all) — the SPA disables Send/Dismiss when this is false, because any
  // write from here would be refused with 403 "loopback only" regardless.
  is_local: boolean
}

export function useFindings() {
  return useQuery<FindingsPayload, Error>({
    queryKey: ["findings"],
    queryFn: async () => (await apiClient.get<FindingsPayload>("/api/findings")).data,
    staleTime: 30_000,
  })
}
