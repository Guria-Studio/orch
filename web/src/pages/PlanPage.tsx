import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "react-router-dom"
import { AlertTriangle } from "lucide-react"
import { PlanIdea } from "@/components/PlanIdea"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { flowStep, useFlow } from "@/hooks/useFlow"
import { t } from "@/i18n"
import { describeLoadError } from "@/lib/errors"

const panel = "rounded-lg border bg-card p-5"

/**
 * Work › Plan (#548): the welcome page's plan and apply steps inside the
 * project — claude plans an idea headless with orch-plan, the operator reviews
 * the tasks, applies them, and runs them from Now after its estimate.
 */
export function PlanPage() {
  const qc = useQueryClient()
  const { data: s, isLoading, isError, error } = useFlow()
  const step = useMutation({
    mutationFn: ({ name, body }: { name: "plan" | "apply"; body?: object }) => flowStep(name, body),
    onSuccess: (data) => qc.setQueryData(["flow"], data),
  })

  if (isLoading) return <Skeleton className="h-48 w-full" />
  if (isError || !s) {
    return (
      <Alert variant="destructive">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("plan.load_failed")}</AlertTitle>
        <AlertDescription>{describeLoadError(error)}</AlertDescription>
      </Alert>
    )
  }
  const busy = step.isPending || s.planning
  const hasTasks = s.tasks.length > 0

  return (
    <div className="mx-auto max-w-2xl space-y-4 text-[13px]">
      <header className="space-y-1">
        <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("plan.title")}</h1>
        <p className="text-muted-foreground">{t("plan.intro")}</p>
      </header>

      <section className={panel}>
        <PlanIdea s={s} disabled={false} busy={busy} onPlan={(idea) => step.mutate({ name: "plan", body: { idea } })} />
      </section>

      {hasTasks && !s.planning ? (
        <section className={panel}>
          {!s.applied ? (
            <>
              <p className="mb-3 text-muted-foreground">{t("welcome.apply_hint")}</p>
              <Button disabled={busy} onClick={() => step.mutate({ name: "apply" })}>
                {t("welcome.apply")}
              </Button>
            </>
          ) : (
            <>
              <pre className="mb-3 whitespace-pre-wrap font-mono text-[11px]">{s.estimate}</pre>
              <Link to="/" className="inline-flex h-8 items-center rounded-md bg-brand px-3 text-white hover:bg-brand/90">
                {t("plan.run_from_now")}
              </Link>
            </>
          )}
        </section>
      ) : null}

      {step.error ? (
        <p role="alert" className="whitespace-pre-wrap text-status-failed">
          {step.error.message}
        </p>
      ) : null}
    </div>
  )
}
