import { useQuery } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"

// GET /api/tools (internal/dashboard/toolsroutes.go, doctor.Tools): every
// program orch runs on this machine.

export type ToolGroup = "agents" | "repo" | "share" | "quality"

export interface Tool {
  id: string
  group: ToolGroup
  /** This project needs it: its tasks route there, PRs, tunnel or E2E are on. */
  used: boolean
  found: boolean
  version?: string
  path?: string
  /** Shell lines to copy, newline-separated; orch never runs them. */
  install?: string
  docs_url?: string
}

/** A config.yaml shell command whose program is not installed (doctor.UnresolvedCommand). */
export interface UnresolvedCommand {
  key: string
  command: string
  detail: string
}

export function useTools() {
  return useQuery<{ tools: Tool[]; commands?: UnresolvedCommand[] }, Error>({
    queryKey: ["tools"],
    queryFn: async () => (await apiClient.get<{ tools: Tool[]; commands?: UnresolvedCommand[] }>("/api/tools")).data,
    // Probing every CLI takes a few seconds; installs happen by hand, and
    // "Check again" refetches.
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  })
}
