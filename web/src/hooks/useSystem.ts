import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { apiClient } from "@/lib/api"

// System › Skills (GET /api/skills) and System › Settings
// (GET /api/config/effective), both read-only.

export type SkillState = "current" | "missing" | "different"

export interface SkillRow {
  name: string
  description: string
  targets: { target: string; state: SkillState; path: string }[]
}

export function useSkills() {
  return useQuery<{ skills: SkillRow[] }, Error>({
    queryKey: ["skills"],
    queryFn: async () => (await apiClient.get<{ skills: SkillRow[] }>("/api/skills")).data,
  })
}

export interface EffectiveConfig {
  path: string
  sections: { name: string; entries: { key: string; value: string }[] }[]
  sources: string[]
  warnings: string[]
  /** config.yaml does not load. */
  error?: string
}

export function useEffectiveConfig() {
  return useQuery<EffectiveConfig, Error>({
    queryKey: ["config-effective"],
    queryFn: async () => (await apiClient.get<EffectiveConfig>("/api/config/effective")).data,
  })
}

/**
 * Saves Settings through PATCH /api/config — config.Set, the function behind
 * `orch config set` — and puts the config it answers with in the cache. A
 * refusal rejects with the server's own reason.
 */
export function useSaveConfig() {
  const queryClient = useQueryClient()
  return useMutation<EffectiveConfig, Error, Record<string, string>>({
    mutationFn: async (set) => {
      try {
        return (await apiClient.patch<EffectiveConfig>("/api/config", { set })).data
      } catch (err) {
        const detail = (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail
        throw new Error(detail ?? (err instanceof Error ? err.message : "the settings were not saved"))
      }
    },
    onSuccess: (data) => queryClient.setQueryData(["config-effective"], data),
  })
}
