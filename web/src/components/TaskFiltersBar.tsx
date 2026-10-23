import { useCallback, useEffect, useState, type ReactNode } from "react"
import { useSearchParams } from "react-router-dom"
import { Search } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { t } from "@/i18n"
import { STATUS } from "@/lib/status"
import { cn } from "@/lib/utils"
import type { TaskFilters } from "@/lib/types"

export interface TaskFiltersBarValue {
  phase: string
  model: string
  search: string
  status: string
}

export interface TaskFiltersBarProps {
  value: TaskFiltersBarValue
  onChange: (next: TaskFiltersBarValue) => void
  /** Options derived from the currently-visible task list. */
  phaseOptions: number[]
  modelOptions: string[]
}

// Values are the API's spellings; labels and shapes come from the shared scale.
const STATUS_OPTIONS = [
  { value: "backlog", meta: STATUS.backlog },
  { value: "todo", meta: STATUS.todo },
  { value: "in-progress", meta: STATUS.in_progress },
  { value: "blocked", meta: STATUS.blocked },
  { value: "done", meta: STATUS.done },
]

// "" is "no filter" in the URL; Radix keeps "" for "nothing chosen", so the
// "all" choice has its own value.
const ALL = "__all"

/** A filter as a compact chip: dashed "+ Status" until set, then its value. */
function FilterChip({
  label,
  value,
  onChange,
  children,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  children: ReactNode
}) {
  return (
    <Select value={value} onValueChange={(v) => onChange(v === ALL ? "" : v)}>
      <SelectTrigger
        aria-label={label}
        className={cn(
          "h-7 w-auto gap-1.5 px-2.5 text-xs max-md:h-9 [&>svg]:h-3 [&>svg]:w-3",
          value ? "border-solid border-input text-foreground" : "border-dashed text-muted-foreground",
        )}
      >
        <SelectValue placeholder={`+ ${label}`} />
      </SelectTrigger>
      <SelectContent>{children}</SelectContent>
    </Select>
  )
}

/**
 * Shared filters bar used by Kanban + Tasks pages: a compact search and one
 * chip per filter. Owns only presentation — the parent owns the
 * `TaskFilters` state and the debounce mapping.
 */
export function TaskFiltersBar({ value, onChange, phaseOptions, modelOptions }: TaskFiltersBarProps) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="relative w-full sm:w-64">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          type="search"
          value={value.search}
          onChange={(e) => onChange({ ...value, search: e.target.value })}
          placeholder={t("filters.search")}
          aria-label={t("filters.search")}
          className="h-7 pl-8 text-xs max-md:h-9"
        />
      </div>
      <FilterChip label={t("filters.status")} value={value.status} onChange={(v) => onChange({ ...value, status: v })}>
        <SelectItem value={ALL}>{t("filters.all_statuses")}</SelectItem>
        {STATUS_OPTIONS.map((s) => (
          <SelectItem key={s.value} value={s.value}>
            <span className="inline-flex items-center gap-2">
              <s.meta.icon className={cn("h-3.5 w-3.5", s.meta.text)} aria-hidden />
              {t(s.meta.label)}
            </span>
          </SelectItem>
        ))}
      </FilterChip>
      <FilterChip label={t("filters.phase_label")} value={value.phase} onChange={(v) => onChange({ ...value, phase: v })}>
        <SelectItem value={ALL}>{t("filters.all_phases")}</SelectItem>
        {phaseOptions.map((p) => (
          <SelectItem key={p} value={String(p)}>
            {t("filters.phase", { phase: p })}
          </SelectItem>
        ))}
      </FilterChip>
      <FilterChip label={t("filters.model")} value={value.model} onChange={(v) => onChange({ ...value, model: v })}>
        <SelectItem value={ALL}>{t("filters.all_models")}</SelectItem>
        {modelOptions.map((m) => (
          <SelectItem key={m} value={m}>
            {m}
          </SelectItem>
        ))}
      </FilterChip>
    </div>
  )
}

const FILTER_PARAMS = { search: "q", status: "status", phase: "phase", model: "model" } as const

/**
 * The filters as query parameters, so a filtered view is a link and List,
 * Kanban and Graph share one set: switching tabs carries the query string.
 */
export function useTaskFilterParams(): [TaskFiltersBarValue, (next: TaskFiltersBarValue) => void] {
  const [params, setParams] = useSearchParams()
  const value: TaskFiltersBarValue = {
    search: params.get(FILTER_PARAMS.search) ?? "",
    status: params.get(FILTER_PARAMS.status) ?? "",
    phase: params.get(FILTER_PARAMS.phase) ?? "",
    model: params.get(FILTER_PARAMS.model) ?? "",
  }
  const setValue = useCallback(
    (next: TaskFiltersBarValue) => {
      setParams(
        (prev) => {
          const out = new URLSearchParams(prev)
          for (const [field, key] of Object.entries(FILTER_PARAMS)) {
            const v = next[field as keyof TaskFiltersBarValue]
            if (v) out.set(key, v)
            else out.delete(key)
          }
          return out
        },
        { replace: true },
      )
    },
    [setParams],
  )
  return [value, setValue]
}

/**
 * Debounce a value by `delayMs`.
 */
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs)
    return () => clearTimeout(timer)
  }, [value, delayMs])
  return debounced
}

/**
 * Map `TaskFiltersBarValue` to the backend `TaskFilters` shape.
 */
export function toTaskFilters(v: TaskFiltersBarValue): TaskFilters {
  const f: TaskFilters = {}
  if (v.phase) f.phase = Number(v.phase)
  if (v.model) f.model = v.model
  if (v.search.trim()) f.q = v.search.trim()
  if (v.status) f.status = v.status
  return f
}
