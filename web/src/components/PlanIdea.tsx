import { useState } from "react"
import { Loader2 } from "lucide-react"
import { StepRow } from "@/components/ActivityFeed"
import { Button } from "@/components/ui/button"
import { t } from "@/i18n"
import type { WelcomeState } from "@/pages/WelcomePage"

const CLAUDE_INSTALL = "https://docs.anthropic.com/en/docs/claude-code"

/**
 * The plan step the welcome page and the project's Plan page share (#548):
 * the idea, claude planning it live, its error, and the tasks it planned.
 * Without claude it says why planning needs it, and what works without it.
 */
export function PlanIdea({
  s,
  disabled,
  busy,
  onPlan,
}: {
  s: Pick<WelcomeState, "clis" | "planning" | "steps" | "plan_error" | "tasks">
  disabled: boolean
  busy: boolean
  onPlan: (idea: string) => void
}) {
  const [idea, setIdea] = useState("")
  const claude = s.clis.find((c) => c.name === "claude")?.installed ?? false
  const hasTasks = s.tasks.length > 0
  return (
    <>
      <textarea
        rows={3}
        value={idea}
        onChange={(e) => setIdea(e.target.value)}
        placeholder={t("welcome.idea_placeholder")}
        aria-label={t("welcome.plan")}
        disabled={disabled || busy}
        className="w-full rounded-md border border-input bg-background px-2.5 py-1.5 text-[13px] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      />
      <Button className="mt-3" variant={hasTasks ? "outline" : "default"} disabled={disabled || !claude || busy} onClick={() => onPlan(idea)}>
        {s.planning ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" aria-hidden /> : null}
        {s.planning ? t("welcome.planning") : hasTasks ? t("welcome.replan") : t("welcome.plan_button")}
      </Button>
      {!claude ? (
        <p className="mt-3 text-muted-foreground">
          {t("welcome.need_claude")}{" "}
          <a href={CLAUDE_INSTALL} target="_blank" rel="noreferrer" className="text-brand underline">
            {t("welcome.install_claude")}
          </a>
        </p>
      ) : null}
      {s.steps.length > 0 ? (
        <ol aria-label={t("activity.title")} aria-live="polite" className="mt-3 max-h-64 space-y-1 overflow-auto font-mono text-[11px] leading-relaxed">
          {s.steps.map((st, i) => (
            <StepRow key={i} step={st} wrap={false} />
          ))}
        </ol>
      ) : null}
      {s.plan_error ? (
        <p role="alert" className="mt-3 whitespace-pre-wrap text-status-failed">
          {s.plan_error}
        </p>
      ) : null}
      {hasTasks && !s.planning ? (
        <ul className="mt-3 divide-y rounded-md border" aria-label={t("welcome.tasks")}>
          {s.tasks.map((task) => (
            <li key={task.id} className="flex items-baseline gap-3 px-3 py-1.5">
              <span className="shrink-0 font-mono text-xs text-muted-foreground">{task.id}</span>
              <span className="min-w-0 flex-1 truncate">{task.title}</span>
              <span className="shrink-0 font-mono text-[11px] text-muted-foreground">{task.model}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </>
  )
}
