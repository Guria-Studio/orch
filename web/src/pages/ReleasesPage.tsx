import { useEffect, useMemo } from "react"
import DOMPurify from "dompurify"
import { marked } from "marked"
import { AlertTriangle, ExternalLink } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { CopyCommand } from "@/components/CopyCommand"
import { markReleasesSeen, useReleases, type ReleaseItem } from "@/hooks/useReleases"
import { fmt, t } from "@/i18n"
import { describeLoadError } from "@/lib/errors"

// Release notes come from GitHub; nothing executable reaches the page.
function notesHTML(markdown: string): string {
  return DOMPurify.sanitize(marked.parse(markdown, { async: false }) as string)
}

function Release({ r }: { r: ReleaseItem }) {
  const html = useMemo(() => notesHTML(r.notes), [r.notes])
  return (
    <li className="rounded-lg border bg-card p-3.5" data-testid={`release-${r.tag}`}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="flex flex-wrap items-center gap-2 text-base font-semibold">
          <a href={r.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 hover:underline">
            {r.tag}
            <ExternalLink className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
          </a>
          {r.current ? <Badge>{t("releases.current")}</Badge> : null}
          {r.newer ? <Badge variant="outline">{t("releases.available")}</Badge> : null}
          {r.critical ? <Badge variant="destructive">{t("releases.critical")}</Badge> : null}
        </h2>
        {r.published_at ? <span className="text-xs text-muted-foreground">{fmt.day(r.published_at)}</span> : null}
      </div>
      {r.notes.trim() ? (
        <div
          className="prose prose-sm dark:prose-invert prose-code:before:content-none prose-code:after:content-none prose-headings:text-base mt-3 max-w-none break-words text-sm [&_a]:text-brand [&_li]:my-1 [&_ul]:list-disc [&_ul]:pl-5"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <p className="mt-3 text-[13px] text-muted-foreground">{t("releases.no_notes")}</p>
      )}
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
    <div className="space-y-6">
      <div className="space-y-1">
        {title}
        <p className="text-[13px] text-muted-foreground">
          {current ? t("releases.running", { version: current }) : null}
          {newer > 0 ? ` ${t("releases.newer", { count: newer })}` : null}
        </p>
      </div>
      {newer > 0 ? <CopyCommand command="orch upgrade" /> : null}
      {data.error ? (
        <Alert>
          <AlertTriangle className="h-4 w-4" aria-hidden />
          <AlertDescription>{t("releases.unavailable", { reason: data.error })}</AlertDescription>
        </Alert>
      ) : null}
      {data.releases.length === 0 ? (
        <p className="rounded-lg border border-dashed px-3.5 py-5 text-[13px] text-muted-foreground">{t("releases.empty")}</p>
      ) : (
        <ul className="space-y-4">
          {data.releases.map((r) => (
            <Release key={r.tag} r={r} />
          ))}
        </ul>
      )}
    </div>
  )
}
