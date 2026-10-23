import { useEffect, useMemo } from "react"
import DOMPurify from "dompurify"
import { marked } from "marked"
import { AlertTriangle } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { markReleasesSeen, useReleases, type ReleaseItem } from "@/hooks/useReleases"
import { fmt, t } from "@/i18n"
import { describeLoadError } from "@/lib/errors"
import { cn } from "@/lib/utils"

// Release notes come from GitHub; nothing executable reaches the page.
function notesHTML(markdown: string): string {
  return DOMPurify.sanitize(marked.parse(markdown, { async: false }) as string)
}

const chip = "inline-flex items-center rounded-md px-1.5 py-0.5 text-[11px] font-medium"

function Release({ r }: { r: ReleaseItem }) {
  const html = useMemo(() => notesHTML(r.notes), [r.notes])
  return (
    <li className="grid gap-x-6 gap-y-2 py-5 sm:grid-cols-[9rem_minmax(0,1fr)]" data-testid={`release-${r.tag}`}>
      <div className="flex items-baseline gap-2 sm:flex-col sm:gap-0.5">
        <h2 className="text-base font-semibold">
          <a href={r.url} target="_blank" rel="noreferrer" className="hover:underline">
            {r.tag}
          </a>
        </h2>
        {r.published_at ? <span className="text-xs text-muted-foreground">{fmt.day(r.published_at)}</span> : null}
      </div>
      <div className="min-w-0 space-y-2">
        {r.current || r.newer || r.critical ? (
          <div className="flex flex-wrap gap-1.5">
            {r.current ? <span className={cn(chip, "bg-brand/15 text-brand")}>{t("releases.current")}</span> : null}
            {r.newer ? <span className={cn(chip, "bg-status-done/14 text-status-done")}>{t("releases.available")}</span> : null}
            {r.critical ? <span className={cn(chip, "bg-status-failed/14 text-status-failed")}>{t("releases.critical")}</span> : null}
          </div>
        ) : null}
        {r.notes.trim() ? (
          <div
            className="prose prose-sm dark:prose-invert prose-code:before:content-none prose-code:after:content-none prose-headings:text-sm max-w-none break-words text-[13px] text-muted-foreground [&_a]:text-brand [&_li]:my-1 [&_ul]:list-disc [&_ul]:pl-5"
            dangerouslySetInnerHTML={{ __html: html }}
          />
        ) : (
          <p className="text-[13px] text-muted-foreground">{t("releases.no_notes")}</p>
        )}
      </div>
    </li>
  )
}

/** What each orch release brought, newest first, with the running one marked. */
export function ReleasesPage() {
  const { data, isLoading, isError, error } = useReleases()
  const current = data?.current ?? ""
  useEffect(() => {
    if (current) markReleasesSeen(current)
  }, [current])

  const title = <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("releases.title")}</h1>
  if (isLoading) {
    return (
      <div className="space-y-6">
        {title}
        <Skeleton className="h-40 w-full rounded-lg" />
      </div>
    )
  }
  if (isError || !data) {
    return (
      <div className="space-y-6">
        {title}
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" aria-hidden />
          <AlertDescription>{describeLoadError(error)}</AlertDescription>
        </Alert>
      </div>
    )
  }
  const newer = data.releases.filter((r) => r.newer).length
  return (
    <div className="space-y-4 text-[13px]">
      <div className="space-y-1">
        {title}
        <p className="text-[13px] text-muted-foreground">
          {current ? t("releases.running", { version: current }) : null}
          {newer > 0 ? ` ${t("releases.newer", { count: newer })}` : null}
        </p>
      </div>
      {data.error ? (
        <Alert>
          <AlertTriangle className="h-4 w-4" aria-hidden />
          <AlertDescription>{t("releases.unavailable", { reason: data.error })}</AlertDescription>
        </Alert>
      ) : null}
      {data.releases.length === 0 ? (
        <p className="rounded-lg border border-dashed px-3.5 py-5 text-[13px] text-muted-foreground">{t("releases.empty")}</p>
      ) : (
        <ul className="divide-y">
          {data.releases.map((r) => (
            <Release key={r.tag} r={r} />
          ))}
        </ul>
      )}
    </div>
  )
}
