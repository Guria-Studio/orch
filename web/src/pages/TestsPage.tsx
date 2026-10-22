import { useState } from "react"
import { AlertTriangle, CircleCheck, CircleDashed, CircleX, Download, FlaskConical, TriangleAlert } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { useTicker } from "@/hooks/useNow"
import { useTestReport, useTestRuns } from "@/hooks/useTests"
import { describeLoadError } from "@/lib/errors"
import { attachment, attachmentUrl, byAttention, runKey, type E2ERun, type E2EStatus, type E2ETest } from "@/lib/tests"
import { formatRelative } from "@/lib/time"
import { t } from "@/i18n"
import { cn } from "@/lib/utils"

const STATUS: Record<E2EStatus, { label: `tests.status.${E2EStatus}`; variant: "success" | "danger" | "warning" | "muted"; icon: typeof CircleCheck }> = {
  passed: { label: "tests.status.passed", variant: "success", icon: CircleCheck },
  failed: { label: "tests.status.failed", variant: "danger", icon: CircleX },
  flaky: { label: "tests.status.flaky", variant: "warning", icon: TriangleAlert },
  skipped: { label: "tests.status.skipped", variant: "muted", icon: CircleDashed },
}

const ENABLE_SNIPPET = `verify:
  enabled: true
  e2e: [pnpm exec playwright test]
  e2e_report: e2e-report.json`

const seconds = (ms: number) => `${(ms / 1000).toFixed(1)}s`

const counts = (passed: number, failed: number) =>
  `${t("tests.passed_count", { count: passed })} · ${t("tests.failed_count", { count: failed })}`

/**
 * Tests (operator only): every E2E run the local verify made on a task
 * attempt, newest first, and for the one picked each test with what a failing
 * one left behind — the error, the step it was in, its screenshot, video and
 * trace.
 */
export function TestsPage() {
  const runs = useTestRuns()
  const now = useTicker(60_000)
  const [picked, setPicked] = useState<string | null>(null)
  const list = runs.data?.runs ?? []
  const selected = list.find((r) => runKey(r) === picked) ?? list[0]

  return (
    <div className="space-y-8">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-[-0.02em]">{t("tests.title")}</h1>
        <p className="max-w-3xl text-sm text-muted-foreground">{t("tests.intro")}</p>
      </header>

      {runs.isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("tests.load_failed")}</AlertTitle>
          <AlertDescription>{describeLoadError(runs.error)}</AlertDescription>
        </Alert>
      ) : null}

      {runs.isLoading ? <Skeleton className="h-48 w-full" /> : null}

      {runs.data && list.length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <FlaskConical className="h-4 w-4" />
              {t("tests.empty_title")}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm text-muted-foreground">
            <p>{t("tests.empty_body")}</p>
            <pre className="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs text-foreground">{ENABLE_SNIPPET}</pre>
          </CardContent>
        </Card>
      ) : null}

      {selected ? (
        <div className="grid gap-6 lg:grid-cols-[18rem_minmax(0,1fr)]">
          <nav aria-label={t("tests.runs")} className="space-y-1">
            <h2 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">{t("tests.runs")}</h2>
            {list.map((r) => (
              <RunRow key={runKey(r)} run={r} now={now} active={runKey(r) === runKey(selected)} onPick={() => setPicked(runKey(r))} />
            ))}
          </nav>
          <RunReport run={selected} />
        </div>
      ) : null}
    </div>
  )
}

function RunRow({ run, now, active, onPick }: { run: E2ERun; now: number; active: boolean; onPick: () => void }) {
  return (
    <button
      type="button"
      onClick={onPick}
      aria-current={active ? "true" : undefined}
      data-testid={`run-${run.task_id}-${run.attempt}`}
      className={cn(
        "w-full rounded-md border px-3 py-2 text-left text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        active ? "border-primary/40 bg-primary/5" : "border-transparent hover:bg-muted",
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="font-mono font-medium">{run.task_id}</span>
        <span className="text-xs text-muted-foreground">{formatRelative(run.at, now)}</span>
      </div>
      <div className="mt-1 flex flex-wrap items-center gap-1.5 text-xs">
        <span className="text-muted-foreground">{t("tests.attempt", { n: run.attempt })}</span>
        <Badge variant={run.failed > 0 ? "danger" : "success"}>{counts(run.passed, run.failed)}</Badge>
        {run.flaky > 0 ? <Badge variant="warning">{t("tests.flaky_count", { count: run.flaky })}</Badge> : null}
      </div>
    </button>
  )
}

function RunReport({ run }: { run: E2ERun }) {
  const report = useTestReport(run)

  if (report.isError) {
    return (
      <Alert variant="destructive">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("tests.report_failed")}</AlertTitle>
        <AlertDescription>{describeLoadError(report.error)}</AlertDescription>
      </Alert>
    )
  }
  if (!report.data) return <Skeleton className="h-64 w-full" />

  const rep = report.data
  return (
    <section className="min-w-0 space-y-4" aria-label={`${run.task_id} — ${t("tests.attempt", { n: run.attempt })}`}>
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <h2 className="font-mono text-lg font-semibold">{run.task_id}</h2>
        <span className="text-muted-foreground">{t("tests.attempt", { n: run.attempt })}</span>
        <Badge variant={rep.stats.failed > 0 ? "danger" : "success"}>
          {counts(rep.stats.passed, rep.stats.failed)}
        </Badge>
        {rep.stats.skipped > 0 ? <Badge variant="muted">{t("tests.skipped_count", { count: rep.stats.skipped })}</Badge> : null}
        {rep.app_url ? <span className="text-xs text-muted-foreground">{t("tests.app_at", { url: rep.app_url })}</span> : null}
      </div>

      {rep.errors?.length ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("tests.run_errors")}</AlertTitle>
          <AlertDescription>
            {rep.errors.map((e) => (
              <pre key={e} className="whitespace-pre-wrap font-mono text-xs">{e}</pre>
            ))}
          </AlertDescription>
        </Alert>
      ) : null}

      {rep.tests.length === 0 ? <p className="text-sm text-muted-foreground">{t("tests.no_tests")}</p> : null}
      <ul className="space-y-2">
        {byAttention(rep.tests).map((test, i) => (
          <TestRow key={`${test.file}:${test.line}:${test.project ?? ""}:${i}`} run={run} test={test} />
        ))}
      </ul>
    </section>
  )
}

function TestRow({ run, test }: { run: E2ERun; test: E2ETest }) {
  const meta = STATUS[test.status]
  const Icon = meta.icon
  const shot = attachment(test, "screenshot")
  const video = attachment(test, "video")
  const trace = attachment(test, "trace")
  const context = attachment(test, "error-context")
  const open = test.status === "failed" || test.status === "flaky"
  const where = test.location ?? `${test.file}:${test.line}`

  return (
    <li data-testid={`test-${test.title}`}>
      <details open={open} className="group rounded-md border bg-card">
        <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <Icon className={cn("h-4 w-4 shrink-0", test.status === "failed" ? "text-status-failed" : test.status === "passed" ? "text-status-done" : "text-muted-foreground")} aria-hidden />
          <span className="sr-only">{t(meta.label)}</span>
          <span className="min-w-0 flex-1 truncate font-medium">{test.title}</span>
          {test.retries > 0 ? <Badge variant="warning">{t("tests.retries", { count: test.retries })}</Badge> : null}
          <span className="font-mono text-xs text-muted-foreground">{seconds(test.duration_ms)}</span>
        </summary>
        <div className="space-y-3 border-t px-3 py-3 text-sm">
          <p className="font-mono text-xs text-muted-foreground">{where}</p>
          {test.steps?.length ? (
            <div>
              <h3 className="text-xs font-medium text-muted-foreground">{open ? t("tests.last_step") : t("tests.steps")}</h3>
              <ol className="mt-1 list-decimal space-y-0.5 pl-5">
                {test.steps.map((s, i) => (
                  <li key={`${s}-${i}`} className={cn(open && i === test.steps!.length - 1 && "font-medium text-status-failed")}>{s}</li>
                ))}
              </ol>
            </div>
          ) : null}
          {test.error ? <pre className="max-h-72 overflow-auto rounded-md bg-muted p-3 font-mono text-xs">{test.error}</pre> : null}
          {shot || video ? (
            <div className="grid gap-3 md:grid-cols-2">
              {shot?.path ? (
                <figure className="space-y-1">
                  <figcaption className="text-xs text-muted-foreground">{t("tests.screenshot")}</figcaption>
                  <a href={attachmentUrl(run, shot.path)} target="_blank" rel="noreferrer">
                    <img src={attachmentUrl(run, shot.path)} alt={`${t("tests.screenshot")}: ${test.title}`} loading="lazy" className="max-w-full rounded border" />
                  </a>
                </figure>
              ) : null}
              {video?.path ? (
                <figure className="space-y-1">
                  <figcaption className="text-xs text-muted-foreground">{t("tests.video")}</figcaption>
                  <video src={attachmentUrl(run, video.path)} controls preload="metadata" className="max-w-full rounded border" aria-label={`${t("tests.video")}: ${test.title}`} />
                </figure>
              ) : null}
            </div>
          ) : null}
          {trace?.path || context?.path ? (
            <div className="flex flex-wrap items-center gap-3 text-xs">
              {trace?.path ? (
                <a className="inline-flex items-center gap-1 font-medium text-primary underline-offset-2 hover:underline" href={attachmentUrl(run, trace.path, true)}>
                  <Download className="h-3.5 w-3.5" aria-hidden />
                  {t("tests.download_trace")}
                </a>
              ) : null}
              {trace ? (
                <span className="text-muted-foreground">
                  {t("tests.trace_hint")} <code className="font-mono">npx playwright show-trace {trace.source}</code>
                </span>
              ) : null}
              {context?.path ? (
                <a className="font-medium text-primary underline-offset-2 hover:underline" href={attachmentUrl(run, context.path)} target="_blank" rel="noreferrer">
                  {t("tests.error_context")}
                </a>
              ) : null}
            </div>
          ) : null}
        </div>
      </details>
    </li>
  )
}
