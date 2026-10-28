import { useEffect, useState, type ReactNode } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CheckCircle2, Circle, Loader2, XCircle } from "lucide-react"
import { StepRow } from "@/components/ActivityFeed"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import type { ActivityStep } from "@/hooks/useTaskActivity"
import { t } from "@/i18n"
import { apiClient } from "@/lib/api"
import { cn } from "@/lib/utils"

/** `GET /api/welcome` (internal/cli/welcome.go welcomeState). */
export interface WelcomeState {
  clis: { name: string; installed: boolean }[]
  cwd: string
  root: string
  log: string[]
  tasks: { id: string; title: string; phase: number; model: string }[]
  planning: boolean
  steps: ActivityStep[]
  plan_error?: string
  diff?: string
  applied: boolean
  estimate?: string
  ready: number
  dashboard_url?: string
}

const CLAUDE_INSTALL = "https://docs.anthropic.com/en/docs/claude-code"

async function welcomeStep(step: string, body?: object): Promise<WelcomeState> {
  try {
    const { data } = await apiClient.post<WelcomeState>(`/api/welcome/${step}`, body)
    return data
  } catch (err) {
    const detail = (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail
    throw new Error(detail ?? (err instanceof Error ? err.message : step))
  }
}

function Section({ n, title, done, active, children }: { n: number; title: string; done: boolean; active: boolean; children: ReactNode }) {
  const Icon = done ? CheckCircle2 : Circle
  return (
    <section className={cn("rounded-lg border bg-card p-5", !active && !done && "opacity-50")} aria-disabled={!active && !done}>
      <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold">
        <Icon className={cn("h-4 w-4", done ? "text-status-done" : "text-muted-foreground")} aria-hidden />
        <span className="text-muted-foreground">{n}.</span> {title}
      </h2>
      {children}
    </section>
  )
}

/**
 * What `orch dashboard` serves outside a project: `orch start` as four steps
 * on a page — the CLIs, the folder, the plan, the first run.
 */
export function WelcomePage() {
  const qc = useQueryClient()
  const { data: s, isError } = useQuery({
    queryKey: ["welcome"],
    queryFn: async () => (await apiClient.get<WelcomeState>("/api/welcome")).data,
    refetchInterval: (q) => (q.state.data?.planning ? 1000 : false),
    retry: false,
  })
  // null until edited: the field shows the chosen folder, else the working directory.
  const [edited, setPath] = useState<string | null>(null)
  const [create, setCreate] = useState(true)
  const [idea, setIdea] = useState("")
  useEffect(() => {
    if (s?.dashboard_url) window.location.href = s.dashboard_url
  }, [s?.dashboard_url])

  const step = useMutation({
    mutationFn: ({ name, body }: { name: string; body?: object }) => welcomeStep(name, body),
    onSuccess: (data) => qc.setQueryData(["welcome"], data),
  })

  if (isError) {
    return (
      <div className="mx-auto max-w-xl px-4 pt-24 text-center text-sm">
        <p className="mb-4 text-muted-foreground">{t("welcome.not_here")}</p>
        <a href="/" className="text-brand underline">{t("welcome.open_dashboard")}</a>
      </div>
    )
  }
  if (!s) return <Loader2 className="mx-auto mt-24 h-5 w-5 animate-spin text-muted-foreground" aria-label={t("welcome.loading")} />

  const path = edited ?? (s.root || s.cwd)
  const anyCLI = s.clis.some((c) => c.installed)
  const claude = s.clis.find((c) => c.name === "claude")?.installed ?? false
  const hasProject = s.root !== ""
  const hasTasks = s.tasks.length > 0
  const busy = step.isPending || s.planning

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-4 px-4 py-12 text-[13px]">
      <header className="mb-2 flex flex-col items-center gap-3 text-center">
        <img src="/favicon.svg" alt="" className="h-10 w-10" />
        <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("welcome.title")}</h1>
        <p className="max-w-md leading-5 text-muted-foreground">{t("welcome.lead")}</p>
      </header>

      <Section n={1} title={t("welcome.clis")} done={anyCLI} active>
        <ul className="flex flex-wrap gap-2" aria-label={t("welcome.clis")}>
          {s.clis.map((c) => (
            <li key={c.name} className="inline-flex items-center gap-1.5 rounded-md border px-2 py-1 font-mono text-xs">
              {c.installed ? <CheckCircle2 className="h-3 w-3 text-status-done" aria-label={t("welcome.installed")} /> : <XCircle className="h-3 w-3 text-muted-foreground" aria-label={t("welcome.missing")} />}
              {c.name}
            </li>
          ))}
        </ul>
        {!claude ? (
          <p className="mt-3 text-muted-foreground">
            {t("welcome.need_claude")}{" "}
            <a href={CLAUDE_INSTALL} target="_blank" rel="noreferrer" className="text-brand underline">{t("welcome.install_claude")}</a>
          </p>
        ) : null}
      </Section>

      <Section n={2} title={t("welcome.folder")} done={hasProject} active={anyCLI}>
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            step.mutate({ name: "project", body: { path, git_init: create } })
          }}
        >
          <Input value={path} onChange={(e) => setPath(e.target.value)} aria-label={t("welcome.folder")} className="font-mono" disabled={!anyCLI || busy} />
          <label className="flex items-center gap-2 text-xs">
            <input type="checkbox" checked={create} onChange={(e) => setCreate(e.target.checked)} className="h-4 w-4 accent-[var(--brand)]" />
            {t("welcome.create")}
          </label>
          <Button type="submit" disabled={!anyCLI || busy || !path.trim()} className="self-start bg-brand text-white hover:bg-brand/90">
            {t("welcome.use_folder")}
          </Button>
        </form>
        {s.log.length > 0 ? <pre className="mt-3 max-h-40 overflow-auto whitespace-pre-wrap font-mono text-[11px] text-muted-foreground">{s.log.join("\n")}</pre> : null}
      </Section>

      <Section n={3} title={t("welcome.plan")} done={hasTasks} active={hasProject}>
        <textarea
          rows={3}
          value={idea}
          onChange={(e) => setIdea(e.target.value)}
          placeholder={t("welcome.idea_placeholder")}
          aria-label={t("welcome.plan")}
          disabled={!hasProject || busy}
          className="w-full rounded-md border border-input bg-background px-2.5 py-1.5 text-[13px] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        <Button className="mt-3" variant={hasTasks ? "outline" : "default"} disabled={!hasProject || !claude || busy} onClick={() => step.mutate({ name: "plan", body: { idea } })}>
          {s.planning ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" aria-hidden /> : null}
          {s.planning ? t("welcome.planning") : hasTasks ? t("welcome.replan") : t("welcome.plan_button")}
        </Button>
        {s.steps.length > 0 ? (
          <ol aria-label={t("activity.title")} aria-live="polite" className="mt-3 max-h-64 space-y-1 overflow-auto font-mono text-[11px] leading-relaxed">
            {s.steps.map((st, i) => <StepRow key={i} step={st} wrap={false} />)}
          </ol>
        ) : null}
        {s.plan_error ? <p role="alert" className="mt-3 whitespace-pre-wrap text-status-failed">{s.plan_error}</p> : null}
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
      </Section>

      <Section n={4} title={t("welcome.run")} done={!!s.dashboard_url} active={hasTasks && !s.planning}>
        {!s.applied ? (
          <>
            <p className="mb-3 text-muted-foreground">{t("welcome.apply_hint")}</p>
            <Button disabled={!hasTasks || busy} onClick={() => step.mutate({ name: "apply" })}>{t("welcome.apply")}</Button>
          </>
        ) : (
          <>
            <pre className="mb-3 whitespace-pre-wrap font-mono text-[11px]">{s.estimate}</pre>
            <Button disabled={busy || s.ready === 0 || !!s.dashboard_url} onClick={() => step.mutate({ name: "run" })} className="bg-brand text-white hover:bg-brand/90">
              {t("welcome.run_button", { count: Math.min(s.ready, 2) })}
            </Button>
          </>
        )}
      </Section>

      {step.error ? <p role="alert" className="whitespace-pre-wrap text-status-failed">{step.error.message}</p> : null}
    </div>
  )
}
