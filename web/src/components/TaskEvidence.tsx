import { ShieldCheck } from "lucide-react"
import { useTaskEvidence } from "@/hooks/useTests"
import { t, type MessageKey } from "@/i18n"
import type { Evidence } from "@/lib/evidence"
import { attachmentUrl } from "@/lib/tests"

const VERDICT: Record<Evidence["verdict"], MessageKey> = {
  passed: "evidence.verdict.passed",
  failed: "evidence.verdict.failed",
  review_unavailable: "evidence.verdict.review_unavailable",
}

const seconds = (s: number) => `${s.toFixed(1)}s`

function reviewLine(r: NonNullable<Evidence["review"]>): string {
  if (r.unavailable) return t("evidence.not_reviewed", { reason: r.unavailable })
  const minor = r.findings.length - r.blocking
  const verdict = r.blocking > 0 ? t("evidence.blocking", { count: r.blocking }) : t("evidence.no_blocking")
  return `${t("evidence.reviewed_by", { reviewer: r.reviewer ?? "?" })}: ${verdict}${minor > 0 ? ` · ${t("evidence.minor", { count: minor })}` : ""}`
}

/**
 * What orch's local verify proved about the task's pushed attempt (#520):
 * the checks it ran, the review, the change in numbers and the E2E run, with
 * the walk-throughs of the tests tagged @evidence and their videos. Operator
 * only; nothing when the task has no evidence.
 */
export function TaskEvidence({ taskId }: { taskId: string }) {
  const { data } = useTaskEvidence(taskId)
  if (!data) return null
  const run = { run_id: data.run_id, task_id: data.task_id, attempt: data.attempt }
  const e2e = data.e2e
  return (
    <section aria-labelledby="task-evidence" className="space-y-3 rounded-lg border px-4 py-3">
      <h2 id="task-evidence" className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        <ShieldCheck className="h-3.5 w-3.5" aria-hidden />
        {t("evidence.title")}
      </h2>
      <p className="text-sm font-medium">
        {t(VERDICT[data.verdict])} · {t("tests.attempt", { n: data.attempt })}
        {data.head_sha ? <span className="font-mono"> · {data.head_sha.slice(0, 7)}</span> : null}
      </p>
      {data.checks.length > 0 ? (
        <ul className="space-y-1 text-xs">
          {data.checks.map((c, i) => (
            <li key={i} className="flex gap-2">
              {c.kind ? <span className="w-16 shrink-0 text-muted-foreground">{c.kind}</span> : <span className="w-16 shrink-0" />}
              <code className="min-w-0 flex-1 truncate">{c.command}</code>
              <span className="tabular-nums text-muted-foreground">{seconds(c.duration_s)}</span>
            </li>
          ))}
        </ul>
      ) : null}
      {data.review ? <p className="text-xs">{reviewLine(data.review)}</p> : null}
      {data.diff ? (
        <p className="text-xs text-muted-foreground">
          {t("evidence.diff", { files: data.diff.files, insertions: data.diff.insertions, deletions: data.diff.deletions })}
        </p>
      ) : null}
      {e2e ? (
        <div className="space-y-2">
          <p className="text-xs">
            E2E: {t("tests.passed_count", { count: e2e.passed })} · {t("tests.failed_count", { count: e2e.failed })}
          </p>
          {(e2e.walkthroughs ?? []).map((w) => (
            <div key={w.title} className="space-y-1.5">
              <p className="text-xs font-medium">{w.title}</p>
              {w.steps && w.steps.length > 0 ? (
                <ol className="list-decimal pl-5 text-xs text-foreground/85">
                  {w.steps.map((s, i) => (
                    <li key={i}>{s}</li>
                  ))}
                </ol>
              ) : null}
              {w.video ? (
                <video controls preload="metadata" className="w-full max-w-[560px] rounded border" src={attachmentUrl(run, w.video)} aria-label={t("tests.video")} />
              ) : null}
            </div>
          ))}
        </div>
      ) : null}
    </section>
  )
}
