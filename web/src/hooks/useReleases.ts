import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"

// GET /api/releases (internal/dashboard/releases.go): orch's releases with
// their notes, the running one and the ones `orch upgrade` would bring.

export interface ReleaseItem {
  tag: string
  url: string
  critical: boolean
  notes: string
  published_at: string
  newer: boolean
  current: boolean
}

export interface ReleasesPayload {
  current: string
  releases: ReleaseItem[]
  /** Why the list is short or empty (offline, checks off); the cached list still comes with it. */
  error?: string
}

export function useReleases(enabled = true) {
  return useQuery({
    enabled,
    queryKey: ["releases"],
    queryFn: async () => (await apiClient.get<ReleasesPayload>("/api/releases")).data,
    // The server asks GitHub at most once a day; nothing changes faster.
    staleTime: 60 * 60_000,
  })
}

const SEEN_KEY = "orch.releases.seen"

/** Whether this browser has opened the running version's notes yet. */
export function releasesSeen(version: string): boolean {
  try {
    return window.localStorage.getItem(SEEN_KEY) === version
  } catch {
    return true // Storage blocked: no dot that could never go away.
  }
}

export function markReleasesSeen(version: string) {
  try {
    window.localStorage.setItem(SEEN_KEY, version)
  } catch {
    // Storage blocked: the dot comes back next visit.
  }
}
