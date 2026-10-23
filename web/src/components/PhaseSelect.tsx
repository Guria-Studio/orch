import { useSearchParams } from "react-router-dom"
import { t } from "@/i18n"

/** `?phase=` — the phase a page is narrowed to, "" for the whole project. */
export function usePhaseParam(): [string, (phase: string) => void] {
  const [params, setParams] = useSearchParams()
  const phase = params.get("phase") ?? ""
  const setPhase = (next: string) => {
    const p = new URLSearchParams(params)
    if (next) p.set("phase", next)
    else p.delete("phase")
    setParams(p, { replace: true })
  }
  return [phase, setPhase]
}

/** The design's "Fase" chip: the whole project, or one phase by name. Hidden without phases to pick. */
export function PhaseSelect({ phases }: { phases: { phase: number; name: string }[] }) {
  const [phase, setPhase] = usePhaseParam()
  if (phases.length === 0) return null
  return (
    <label className="inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-xs text-muted-foreground">
      {t("filters.phase_label")}
      <select
        value={phase}
        onChange={(e) => setPhase(e.target.value)}
        className="bg-transparent text-foreground focus-visible:outline-none"
      >
        <option value="">{t("filters.all_phases")}</option>
        {phases.map((p) => (
          <option key={p.phase} value={String(p.phase)}>
            {p.name || t("filters.phase", { phase: p.phase })}
          </option>
        ))}
      </select>
    </label>
  )
}
