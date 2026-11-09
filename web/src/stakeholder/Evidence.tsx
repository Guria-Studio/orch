import { evidenceLine, copy, type Lang } from "./portal"
import type { StakeholderEvidence } from "./types"

/**
 * How a delivered item was checked (#520): one line in the client's words,
 * and, when its E2E walk-through is tagged @evidence, the steps it took and
 * — only if the operator publishes it — its video.
 */
export function DeliverableEvidence({ evidence, lang, mediaSrc }: { evidence: StakeholderEvidence; lang: Lang; mediaSrc: (path: string) => string }) {
  const t = copy[lang]
  const line = evidenceLine(evidence, lang)
  const steps = evidence.steps ?? []
  if (!line && steps.length === 0 && !evidence.video) return null
  return (
    <span className="block text-sm text-[var(--portal-muted)]">
      {line ? <span className="block">{line}</span> : null}
      {steps.length > 0 || evidence.video ? (
        <details className="mt-1">
          <summary className="cursor-pointer underline">{t.evidenceHow}</summary>
          {steps.length > 0 ? (
            <ol className="mt-1 list-decimal pl-5">
              {steps.map((s, i) => (
                <li key={i}>{s}</li>
              ))}
            </ol>
          ) : null}
          {evidence.video ? (
            <video
              controls
              preload="metadata"
              className="mt-2 w-full max-w-[560px] rounded border border-[var(--portal-line)]"
              src={mediaSrc(evidence.video)}
              poster={evidence.poster ? mediaSrc(evidence.poster) : undefined}
              aria-label={t.evidenceVideo}
            />
          ) : null}
        </details>
      ) : null}
    </span>
  )
}
