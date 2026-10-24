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

/** One lesson on System › Knowledge (GET /api/knowledge, #453), with how it did. */
export interface Lesson {
  id: string
  title: string
  area?: string[]
  tags?: string[]
  source: string
  task?: string
  created: string
  body: string
  path: string
  /** Prompts that carried it. */
  prompts: number
  /** Done tasks those prompts were for, and how many of them were a first-try success. */
  tasks: number
  first_try: number
}

/** The lessons, or with a query the ones it finds, best first (orch_knowledge_search's ranking). */
export function useKnowledge(q: string) {
  return useQuery<{ lessons: Lesson[]; sources: string[] }, Error>({
    queryKey: ["knowledge", q],
    queryFn: async () => (await apiClient.get<{ lessons: Lesson[]; sources: string[] }>("/api/knowledge", { params: q ? { q } : {} })).data,
    placeholderData: (prev) => prev,
  })
}
