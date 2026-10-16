import { useEffect, useState } from "react"
import { apiClient } from "@/lib/api"

export interface ActivityStep {
  /** "start" opens an attempt; then "text", "tool", "error", and "end". */
  kind: "start" | "text" | "tool" | "error" | "end"
  tool?: string
  detail?: string
}

interface ActivityPayload {
  offset: number
  steps: ActivityStep[]
  exists: boolean
}

export interface TaskActivity {
  steps: ActivityStep[]
  /** False until the task's first dispatch writes its log. */
  exists: boolean
  loaded: boolean
  failed: boolean
}

// Enough to read back over, not a whole run's transcript.
const KEEP = 200

/** The current attempt's steps: a log keeps every attempt, each opened by "start". */
function sinceLastStart(steps: ActivityStep[]): ActivityStep[] {
  for (let i = steps.length - 1; i >= 0; i--) {
    if (steps[i].kind === "start") return steps.slice(i)
  }
  return steps
}

/**
 * What a task's agent is doing, read off its dispatch log
 * (`/api/task/{id}/activity`). While `live`, it asks every `intervalMs` for
 * what the agent wrote since the last read; otherwise it reads once. The
 * steps belong to one task: mount a new caller (a `key`) for another.
 */
export function useTaskActivity(taskId: string | null, live: boolean, intervalMs = 2_000): TaskActivity {
  const [state, setState] = useState<TaskActivity>({ steps: [], exists: false, loaded: false, failed: false })

  useEffect(() => {
    if (!taskId) return
    let offset: number | null = null
    let cancelled = false
    let timer: number | undefined

    const read = async () => {
      try {
        const { data } = await apiClient.get<ActivityPayload>(`/api/task/${encodeURIComponent(taskId)}/activity`, {
          params: offset === null ? undefined : { offset },
        })
        if (cancelled) return
        // A first read re-reads the tail, and an offset that went backwards
        // means the log was replaced: either way the steps start over.
        const restarted = offset === null || data.offset < offset
        offset = data.offset
        setState((prev) => ({
          steps: sinceLastStart([...(restarted ? [] : prev.steps), ...data.steps]).slice(-KEEP),
          exists: data.exists,
          loaded: true,
          failed: false,
        }))
      } catch {
        if (!cancelled) setState((prev) => ({ ...prev, loaded: true, failed: true }))
      }
      if (live && !cancelled) timer = window.setTimeout(read, intervalMs)
    }
    void read()
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [taskId, live, intervalMs])

  return state
}
