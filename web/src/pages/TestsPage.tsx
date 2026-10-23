import { useState, type ReactNode } from "react"
import { AlertTriangle, Download, FlaskConical } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { useTicker } from "@/hooks/useNow"
import { useTestReport, useTestRuns } from "@/hooks/useTests"
import { describeLoadError } from "@/lib/errors"
import { attachment, attachmentUrl, byAttention, runKey, type E2ERun, type E2EStatus, type E2ETest } from "@/lib/tests"
import { formatRelative } from "@/lib/time"
import { t } from "@/i18n"
import { cn } from "@/lib/utils"

const STATUS: Record<E2EStatus, { label: `tests.status.${E2EStatus}` }> = {
  passed: { label: "tests.status.passed" },
  failed: { label: "tests.status.failed" },
  flaky: { label: "tests.status.flaky" },
  skipped: { label: "tests.status.skipped" },
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
    <div className="space-y-4 text-[13px]">
      {/* The layout's header names the page; the artboard keeps only this line. */}
      <h1 className="sr-only">{t("tests.title")}</h1>
      <p className="text-xs text-muted-foreground">{t("tests.intro")}</p>

      {runs.isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("tests.load_failed")}</AlertTitle>
          <AlertDescription>{describeLoadError(runs.error)}</AlertDescription>
        </Alert>
      ) : null}

      {runs.isLoading ? <Skeleton className="h-48 w-full" /> : null}

      {runs.data && list.length === 0 ? (
        <section className="space-y-3 rounded-lg border bg-card p-4">
          <h2 className="flex items-center gap-2 font-medium">
            <FlaskConical className="h-4 w-4" aria-hidden />
            {t("tests.empty_title")}
          </h2>
          <p className="text-muted-foreground">{t("tests.empty_body")}</p>
          <pre className="overflow-x-auto rounded-md border bg-background p-3 font-mono text-xs">{ENABLE_SNIPPET}</pre>
        </section>
      ) : null}

      {selected ? (
        <div className="flex flex-wrap overflow-hidden rounded-lg border">
          <nav aria-label={t("tests.runs")} className="flex max-w-[340px] flex-[1_1_300px] flex-col border-r max-md:max-w-none max-md:border-b max-md:border-r-0">
            <span className="px-4 pb-2 pt-3 text-xs font-medium text-muted-foreground">{t("tests.runs")}</span>
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

const dot = (cls: string) => <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", cls)} aria-hidden />

function RunRow({ run, now, active, onPick }: { run: E2ERun; now: number; active: boolean; onPick: () => void }) {
  return (
    <button
      type="button"
      onClick={onPick}
      aria-current={active ? "true" : undefined}
      data-testid={`run-${run.task_id}-${run.attempt}`}
      className={cn(
        "flex w-full flex-col gap-1 border-b px-4 py-2.5 text-left transition-colors last:border-b-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
        active ? "bg-secondary" : "hover:bg-secondary/60",
      )}
    >
      <span className="flex items-center gap-2">
        <span className={cn("h-2 w-2 shrink-0 rounded-full", run.failed > 0 ? "bg-status-blocked" : "bg-status-done")} aria-hidden />
        <span className="font-mono text-xs">{run.task_id}</span>
        <span className="text-xs text-muted-foreground">{t("tests.attempt", { n: run.attempt })}</span>
        <span className="ml-auto text-xs text-muted-foreground">{formatRelative(run.at, now)}</span>
      </span>
      <span className="text-xs">
        <span>{counts(run.passed, run.failed)}</span>
        {run.flaky > 0 ? <span> · {t("tests.flaky_count", { count: run.flaky })}</span> : null}
      </span>
    </button>
  )
}

function RunReport({ run }: { run: E2ERun }) {
  const report = useTestReport(run)
  const body = (children: ReactNode) => (
    <section className="flex min-w-0 flex-[999_1_480px] flex-col gap-4 p-5" aria-label={`${run.task_id} — ${t("tests.attempt", { n: run.attempt })}`}>
      {children}
    </section>
  )

  if (report.isError) {
    return body(
      <Alert variant="destructive">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("tests.report_failed")}</AlertTitle>
        <AlertDescription>{describeLoadError(report.error)}</AlertDescription>
      </Alert>,
    )
  }
  if (!report.data) return body(<Skeleton className="h-64 w-full" />)

  const rep = report.data
  return body(
    <>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <h2 className="text-[15px] font-semibold">
          <span className="font-mono">{run.task_id}</span> · {t("tests.attempt", { n: run.attempt })}
        </h2>
        <span className="text-xs text-status-done">{t("tests.passed_count", { count: rep.stats.passed })}</span>
        <span className="text-xs text-status-blocked">{t("tests.failed_count", { count: rep.stats.failed })}</span>
        <span className="text-xs text-status-running">{t("tests.flaky_count", { count: rep.stats.flaky })}</span>
        <span className="text-xs text-muted-foreground">{t("tests.skipped_count", { count: rep.stats.skipped })}</span>
        {rep.app_url ? <span className="ml-auto text-xs text-muted-foreground">{t("tests.app_at", { url: rep.app_url })}</span> : null}
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

      {rep.tests.length === 0 ? <p className="text-muted-foreground">{t("tests.no_tests")}</p> : null}
      {rep.tests.length > 0 ? (
        <ul className="divide-y rounded-lg border">
          {byAttention(rep.tests).map((test, i) => (
            <TestRow key={`${test.file}:${test.line}:${test.project ?? ""}:${i}`} run={run} test={test} />
          ))}
        </ul>
      ) : null}
    </>,
  )
}

const STATUS_TONE: Record<E2EStatus, { text: string; bg: string }> = {
  passed: { text: "text-status-done", bg: "bg-status-done" },
  failed: { text: "text-status-blocked", bg: "bg-status-blocked" },
  flaky: { text: "text-status-running", bg: "bg-status-running" },
  skipped: { text: "text-muted-foreground", bg: "bg-muted-foreground" },
}

// A test is a row (status, name, retries). A failing or flaky one opens on
// what it left behind, as the artboard's "failed test" panel: the error, the
// last step and all of them, the screenshot, the video and the trace.
function TestRow({ run, test }: { run: E2ERun; test: E2ETest }) {
  const tone = STATUS_TONE[test.status]
  const shot = attachment(test, "screenshot")
  const video = attachment(test, "video")
  const trace = attachment(test, "trace")
  const context = attachment(test, "error-context")
  const open = test.status === "failed" || test.status === "flaky"
  const where = test.location ?? `${test.file}:${test.line}`
  const steps = test.steps ?? []

  return (
    <li data-testid={`test-${test.title}`}>
      <details open={open} className="group">
        <summary className="grid h-[38px] cursor-pointer list-none grid-cols-[90px_minmax(0,1fr)_110px] items-center gap-x-3.5 px-3.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
          <span className={cn("inline-flex items-center gap-1.5 text-xs", tone.text)}>
            {dot(tone.bg)}
            {t(STATUS[test.status].label)}
          </span>
          <span className="truncate">{test.title}</span>
          <span className="text-right text-xs text-muted-foreground">{test.retries > 0 ? t("tests.retries", { count: test.retries }) : seconds(test.duration_ms)}</span>
        </summary>
        <article
          aria-label={test.title}
          className={cn("mx-3.5 mb-3.5 flex flex-col gap-3 rounded-lg border bg-card p-4", open && "border-status-blocked/40")}
        >
          <span className="font-medium">{test.title}</span>
          <p className="font-mono text-xs text-muted-foreground">{where}</p>
          {test.error ? (
            <div className="flex flex-col gap-1.5">
              <span className="text-xs text-muted-foreground">{t("tests.error_context")}</span>
              <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-md border bg-background p-2.5 font-mono text-xs text-status-blocked">{test.error}</pre>
            </div>
          ) : null}
          {steps.length || shot?.path ? (
            <div className="grid gap-3 md:grid-cols-2">
              {steps.length ? (
                <div className="flex flex-col gap-1.5">
                  <span className="text-xs text-muted-foreground">{open ? t("tests.last_step") : t("tests.steps")}</span>
                  <span>{steps[steps.length - 1]}</span>
                  {steps.length > 1 ? (
                    <details>
                      <summary className="cursor-pointer text-xs text-muted-foreground">
                        {t("tests.steps")} ({steps.length})
                      </summary>
                      <ol className="mt-1 list-decimal space-y-0.5 pl-5 text-xs">
                        {steps.map((s, i) => (
                          <li key={`${s}-${i}`}>{s}</li>
                        ))}
                      </ol>
                    </details>
                  ) : null}
                </div>
              ) : null}
              {shot?.path ? (
                <figure className="flex flex-col gap-1.5">
                  <figcaption className="text-xs text-muted-foreground">{t("tests.screenshot")}</figcaption>
                  <a href={attachmentUrl(run, shot.path)} target="_blank" rel="noreferrer">
                    <img src={attachmentUrl(run, shot.path)} alt={`${t("tests.screenshot")}: ${test.title}`} loading="lazy" className="max-w-full rounded-md border" />
                  </a>
                </figure>
              ) : null}
            </div>
          ) : null}
          {video?.path ? (
            <figure className="flex flex-col gap-1.5">
              <figcaption className="text-xs text-muted-foreground">{t("tests.video")}</figcaption>
              <video src={attachmentUrl(run, video.path)} controls preload="metadata" className="max-w-full rounded-md border" aria-label={`${t("tests.video")}: ${test.title}`} />
            </figure>
          ) : null}
          {trace?.path || context?.path ? (
            <div className="flex flex-wrap items-center gap-2">
              {trace?.path ? (
                <a
                  className="inline-flex h-7 items-center gap-1.5 rounded-md bg-primary px-2.5 text-xs font-medium text-primary-foreground hover:bg-primary/90"
                  href={attachmentUrl(run, trace.path, true)}
                >
                  <Download className="h-3.5 w-3.5" aria-hidden />
                  {t("tests.download_trace")}
                </a>
              ) : null}
              {context?.path ? (
                <a className="inline-flex h-7 items-center rounded-md border px-2.5 text-xs hover:bg-secondary" href={attachmentUrl(run, context.path)} target="_blank" rel="noreferrer">
                  {t("tests.error_context")}
                </a>
              ) : null}
              {trace ? (
                <span className="text-xs text-muted-foreground">
                  {t("tests.trace_hint")} <code className="font-mono text-foreground">npx playwright show-trace {trace.source}</code>
                </span>
              ) : null}
            </div>
          ) : null}
        </article>
      </details>
    </li>
  )
}
