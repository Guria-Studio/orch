import { useState, type FormEvent } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import type { BudgetRow } from "@/hooks/useBudgetSummary"
import { setBudgetLimits } from "@/hooks/useBudgetLimits"
import { t } from "@/i18n"

/**
 * "Edit limits": each provider's window in the active preset, saved through
 * `orch budget set`'s function (POST /api/budget/{provider}). Only the rows
 * whose numbers changed are sent; a refusal shows the server's reason on its
 * row.
 */
export function BudgetLimitsButton({ preset, rows }: { preset: string; rows: BudgetRow[] }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button type="button" variant="outline" size="sm" onClick={() => setOpen(true)}>
        {t("quota.edit_limits")}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>{t("quota.limits.title")}</DialogTitle>
            <DialogDescription>{t("quota.limits.intro", { preset })}</DialogDescription>
          </DialogHeader>
          <ul className="divide-y rounded-lg border text-[13px]">
            {rows.map((row) => (
              <LimitRow key={row.provider} row={row} />
            ))}
          </ul>
        </DialogContent>
      </Dialog>
    </>
  )
}

function LimitRow({ row }: { row: BudgetRow }) {
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const num = (name: string) => Number(form.get(name))
    const next = { token_budget: num("tokens"), window_hours: num("window"), threshold_pct: num("threshold") }
    const changed = {
      ...(next.token_budget !== row.token_budget ? { token_budget: next.token_budget } : {}),
      ...(next.window_hours !== row.window_hours ? { window_hours: next.window_hours } : {}),
      ...(next.threshold_pct !== row.threshold_pct ? { threshold_pct: next.threshold_pct } : {}),
    }
    if (Object.keys(changed).length === 0) return
    setBusy(true)
    setError(null)
    setSaved(false)
    try {
      await setBudgetLimits(row.provider, changed)
      setSaved(true)
      await queryClient.invalidateQueries({ queryKey: ["budget-summary"] })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const field = "flex flex-col gap-1 text-xs text-muted-foreground"
  return (
    <li className="px-3.5 py-3">
      <form onSubmit={(e) => void submit(e)} aria-label={row.provider} className="grid items-end gap-3 sm:grid-cols-[6rem_minmax(0,1fr)_5rem_5.5rem_auto]">
        <span className="pb-1.5 font-mono text-sm font-medium">{row.provider}</span>
        <label className={field}>
          {t("quota.limits.tokens")}
          <Input name="tokens" type="number" step={1} defaultValue={row.token_budget} inputMode="numeric" />
        </label>
        <label className={field}>
          {t("quota.limits.window")}
          <Input name="window" type="number" step="any" defaultValue={row.window_hours} inputMode="decimal" />
        </label>
        <label className={field}>
          {t("quota.limits.threshold")}
          <Input name="threshold" type="number" step="any" defaultValue={row.threshold_pct} inputMode="decimal" />
        </label>
        <Button type="submit" size="sm" disabled={busy}>
          {t("quota.limits.save")}
        </Button>
      </form>
      {error ? (
        <p role="alert" className="mt-2 text-xs text-status-failed">
          {error}
        </p>
      ) : saved ? (
        <p role="status" className="mt-2 text-xs text-status-done">
          {t("quota.limits.saved")}
        </p>
      ) : null}
    </li>
  )
}
