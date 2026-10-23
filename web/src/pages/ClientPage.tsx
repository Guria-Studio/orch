import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useSearchParams } from "react-router-dom"
import { AlertTriangle, ArrowLeft, ChevronRight, Circle, CircleCheck, CircleDot, CirclePause, FileText, ShieldCheck } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { t as tr, useLanguage } from "@/i18n"
import { apiClient } from "@/lib/api"
import { cn } from "@/lib/utils"
import {
  copy,
  currentPhase,
  deliveriesSince,
  formatDay,
  phaseName,
  phaseState,
  qualityLine,
  readAndRecordVisit,
  relativeTime,
  renderMarkdown,
  statusLine,
  type Lang,
  type PhaseState,
} from "@/stakeholder/portal"
import type { DeliverableStatus, StakeholderMilestone, StakeholderSnapshot } from "@/stakeholder/types"

// How many recent deliveries the overview lists before "show all": the
// newest few are the news; the rest is history the roadmap already holds.
const RECENT_SHOWN = 5

type View = "overview" | "roadmap" | "documents"

/**
 * What a client sees, inside the backoffice: the client portal's snapshot
 * (the one `orch publish` writes, from /stakeholder/data.json) — where the
 * project stands in a sentence, what arrived since the last visit, the
 * roadmap by name and the shared documents. Never task ids, models, tokens or
 * file paths: the snapshot holds none.
 *
 * A stakeholder opening a shared link lands here; the operator opens it to
 * see the same page. The words come from the portal's own dictionary, in the
 * reader's chosen language.
 */
export function ClientPage() {
  const lang = useLanguage() as Lang
  const [params, setParams] = useSearchParams()
  const { data, isLoading, isError, error } = useQuery<StakeholderSnapshot, Error>({
    queryKey: ["client-snapshot"],
    queryFn: async () => (await apiClient.get<StakeholderSnapshot>("/stakeholder/data.json")).data,
    refetchInterval: (query) => ((query.state.data?.refresh_interval_s ?? 0) > 0 ? query.state.data!.refresh_interval_s * 1000 : false),
  })

  if (isLoading) return <Skeleton className="h-64 w-full max-w-3xl" />
  if (isError || !data) {
    return (
      <Alert variant="destructive" className="max-w-3xl">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{copy[lang].loadError}</AlertTitle>
        <AlertDescription>{error?.message ?? tr("common.unknown_error")}</AlertDescription>
      </Alert>
    )
  }

  const t = copy[lang]
  const documents = data.documents ?? []
  const requested = params.get("view") as View | null
  const view: View = requested === "roadmap" || (requested === "documents" && documents.length > 0) ? requested : "overview"
  const views: { id: View; label: string }[] = [
    { id: "overview", label: t.summary },
    { id: "roadmap", label: t.roadmap },
    ...(documents.length > 0 ? [{ id: "documents" as View, label: t.documents }] : []),
  ]
  const name = data.branding?.name?.trim() || data.project_name
  const go = (next: View, doc?: string) => {
    const p = new URLSearchParams(params)
    p.delete("doc")
    if (next === "overview") p.delete("view")
    else p.set("view", next)
    if (doc) p.set("doc", doc)
    setParams(p)
  }

  return (
    <div className="max-w-3xl space-y-8 text-[13px]">
      <header className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            {data.branding?.logo ? <img src={data.branding.logo} alt="" className="h-8 w-auto max-w-[140px] object-contain" /> : null}
            <h1 className="truncate text-xl font-semibold tracking-[-0.01em]">{name}</h1>
          </div>
          <span className="text-xs text-muted-foreground" title={data.generated_at}>
            {t.updated(relativeTime(data.generated_at, lang))}
          </span>
        </div>
        <div role="tablist" aria-label={name} className="flex gap-1">
          {views.map((v) => (
            <button
              key={v.id}
              type="button"
              role="tab"
              aria-selected={view === v.id}
              onClick={() => go(v.id)}
              className={cn(
                "h-7 rounded-md border px-2.5 transition-colors",
                view === v.id ? "border-input bg-secondary font-medium" : "border-transparent text-muted-foreground hover:bg-secondary hover:text-foreground",
              )}
            >
              {v.label}
            </button>
          ))}
        </div>
      </header>

      {view === "overview" ? <Overview data={data} lang={lang} onRoadmap={() => go("roadmap")} /> : null}
      {view === "roadmap" ? <Roadmap data={data} lang={lang} /> : null}
      {view === "documents" ? <Documents data={data} lang={lang} open={params.get("doc") ?? undefined} onOpen={(id) => go("documents", id)} /> : null}
      {data.branding?.footer ? <footer className="border-t pt-4 text-xs text-muted-foreground">{data.branding.footer}</footer> : null}
    </div>
  )
}

function SectionTitle({ id, children }: { id: string; children: string }) {
  return (
    <h3 id={id} className="text-xs font-medium text-muted-foreground">
      {children}
    </h3>
  )
}

function Overview({ data, lang, onRoadmap }: { data: StakeholderSnapshot; lang: Lang; onRoadmap: () => void }) {
  const t = copy[lang]
  const line = statusLine(data, lang)
  const { summary } = data
  const pct = summary.total > 0 ? Math.round((summary.done / summary.total) * 100) : 0
  // Read once per page load: the previous visit is what "since" means.
  const [lastVisit] = useState(() => readAndRecordVisit(data.project_name, new Date()))
  const recent = useMemo(() => deliveriesSince(data.deliveries, lastVisit, new Date(data.generated_at)), [data, lastVisit])
  const cur = currentPhase(data.milestones)
  const [showAll, setShowAll] = useState(false)
  const quality = qualityLine(data.quality, lang)

  return (
    <div className="space-y-8">
      <section aria-labelledby="client-status" className="space-y-3">
        <h2 id="client-status" className="text-2xl font-semibold leading-tight tracking-[-0.02em]">
          {line.headline}
        </h2>
        {line.detail ? <p className="text-sm text-muted-foreground">{line.detail}</p> : null}
        <div className="space-y-1.5">
          <div
            className="h-1.5 overflow-hidden rounded-full bg-secondary"
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={pct}
            aria-label={t.progress(summary.done, summary.total)}
          >
            <div className="h-full rounded-full bg-brand" style={{ width: `${pct}%` }} />
          </div>
          <p className="flex justify-between text-xs text-muted-foreground">
            <span>{t.progress(summary.done, summary.total)}</span>
            <span className="tabular-nums">{pct}%</span>
          </p>
        </div>
      </section>

      {data.blockers.length > 0 ? (
        <section aria-labelledby="client-hold" className="space-y-2">
          <SectionTitle id="client-hold">{t.onHold}</SectionTitle>
          <ul className="divide-y rounded-lg border">
            {data.blockers.map((b, i) => (
              <li key={`${b.phase}-${i}`} className="flex gap-3 px-3.5 py-2.5">
                <CirclePause className="mt-0.5 h-4 w-4 shrink-0 text-status-blocked" aria-hidden />
                <div>
                  <p className="font-medium">{b.title}</p>
                  <p className="text-xs text-muted-foreground">{b.reason}</p>
                </div>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {cur ? (
        <section aria-labelledby="client-now" className="space-y-2">
          <SectionTitle id="client-now">{t.now}</SectionTitle>
          <button type="button" onClick={onRoadmap} className="flex w-full items-center gap-3 rounded-lg border px-3.5 py-3 text-left hover:bg-secondary">
            <div className="min-w-0 flex-1">
              <p className="font-medium">{phaseName(cur.name)}</p>
              <p className="text-xs text-muted-foreground">{t.progress(cur.done, cur.total)}</p>
            </div>
            <ChevronRight className="h-4 w-4 text-muted-foreground" aria-hidden />
          </button>
        </section>
      ) : null}

      <section aria-labelledby="client-since" className="space-y-2">
        <SectionTitle id="client-since">{lastVisit ? t.since : t.sinceFirst}</SectionTitle>
        {recent.length === 0 ? (
          <p className="text-muted-foreground">{t.nothingNew}</p>
        ) : (
          <ul className="divide-y rounded-lg border">
            {(showAll ? recent : recent.slice(0, RECENT_SHOWN)).map((d, i) => (
              <li key={`${d.finished_at}-${i}`} className="flex items-start gap-3 px-3.5 py-2.5">
                <CircleCheck className="mt-0.5 h-4 w-4 shrink-0 text-status-done" aria-hidden />
                <div className="min-w-0 flex-1">
                  <p className="font-medium">{d.title}</p>
                  <p className="text-xs text-muted-foreground">{phaseName(d.phase)}</p>
                </div>
                <time dateTime={d.finished_at} className="shrink-0 text-xs text-muted-foreground">
                  {formatDay(d.finished_at, lang)}
                </time>
              </li>
            ))}
          </ul>
        )}
        {recent.length > RECENT_SHOWN ? (
          <button type="button" onClick={() => setShowAll((v) => !v)} aria-expanded={showAll} className="text-xs font-medium text-brand hover:underline">
            {showAll ? t.showLess : t.showAll(recent.length)}
          </button>
        ) : null}
      </section>

      {data.quality ? (
        <section aria-labelledby="client-quality" className="space-y-2">
          <SectionTitle id="client-quality">{t.quality}</SectionTitle>
          <ul className="divide-y rounded-lg border">
            {data.quality.gates.map((gate) => (
              <li key={gate} className="flex items-center gap-3 px-3.5 py-2.5">
                <ShieldCheck className="h-4 w-4 shrink-0 text-status-done" aria-hidden />
                <span>{t.gates[gate]}</span>
              </li>
            ))}
          </ul>
          {quality ? <p className="text-xs text-muted-foreground">{quality}</p> : null}
        </section>
      ) : null}

      {data.budget.enabled && data.budget.spend_usd != null ? (
        <section aria-labelledby="client-budget" className="space-y-1">
          <SectionTitle id="client-budget">{t.budget}</SectionTitle>
          <p className="text-base">
            {t.spent}: ${data.budget.spend_usd.toFixed(2)}
          </p>
        </section>
      ) : null}
    </div>
  )
}

function Roadmap({ data, lang }: { data: StakeholderSnapshot; lang: Lang }) {
  const t = copy[lang]
  const phases = [...data.milestones].sort((a, b) => a.phase - b.phase)
  const cur = currentPhase(phases)
  return (
    <section aria-label={t.roadmap}>
      <ol>
        {phases.map((m, i) => (
          <PhaseRow key={m.phase} m={m} state={phaseState(m, cur)} lang={lang} last={i === phases.length - 1} />
        ))}
      </ol>
    </section>
  )
}

function PhaseRow({ m, state, lang, last }: { m: StakeholderMilestone; state: PhaseState; lang: Lang; last: boolean }) {
  const t = copy[lang]
  const stateLabel = { done: t.done, now: t.now, next: t.next }[state]
  const marker =
    state === "done" ? (
      <CircleCheck className="h-5 w-5 text-status-done" aria-hidden />
    ) : state === "now" ? (
      <CircleDot className="h-5 w-5 text-brand" aria-hidden />
    ) : (
      <Circle className="h-5 w-5 text-muted-foreground" aria-hidden />
    )
  return (
    <li className="flex gap-3">
      <div className="flex flex-col items-center">
        <div className="bg-background py-0.5">{marker}</div>
        {last ? null : <div className="w-px flex-1 bg-border" />}
      </div>
      <details open={state === "now"} className="group mb-5 min-w-0 flex-1">
        <summary className="flex cursor-pointer list-none items-start gap-2 py-0.5">
          <div className="min-w-0 flex-1">
            <p className="text-base font-medium">{phaseName(m.name)}</p>
            <p className="text-xs text-muted-foreground">
              {stateLabel} · {t.progress(m.done, m.total)}
              {m.blocked > 0 ? ` · ${m.blocked} ${t.onHold.toLowerCase()}` : ""}
            </p>
          </div>
          <ChevronRight className="mt-1 h-4 w-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" aria-hidden />
        </summary>
        <div className="mt-3 space-y-4">
          {(m.packages ?? []).map((p, i) => (
            <div key={`${p.name}-${i}`} className="space-y-1.5">
              <p className="text-xs font-medium text-muted-foreground">
                {p.name ? p.name.charAt(0).toUpperCase() + p.name.slice(1) : t.other} · {p.done}/{p.total}
              </p>
              <ul className="space-y-1">
                {p.deliverables.map((d, j) => (
                  <li key={`${d.title}-${j}`} className="flex items-start gap-2">
                    <StatusIcon status={d.status} lang={lang} />
                    <span className={d.status === "done" ? "text-muted-foreground" : ""}>{d.title}</span>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      </details>
    </li>
  )
}

function StatusIcon({ status, lang }: { status: DeliverableStatus; lang: Lang }) {
  const t = copy[lang]
  const common = "mt-0.5 h-3.5 w-3.5 shrink-0"
  switch (status) {
    case "done":
      return <CircleCheck className={`${common} text-status-done`} aria-label={t.done} />
    case "in_progress":
      return <CircleDot className={`${common} text-brand`} aria-label={t.inProgress} />
    case "blocked":
      return <CirclePause className={`${common} text-status-blocked`} aria-label={t.blocked} />
    default:
      return <Circle className={`${common} text-muted-foreground`} aria-label={t.pending} />
  }
}

function Documents({ data, lang, open, onOpen }: { data: StakeholderSnapshot; lang: Lang; open?: string; onOpen: (id?: string) => void }) {
  const t = copy[lang]
  const documents = data.documents ?? []
  const doc = open ? documents.find((d) => d.id === open) : undefined
  const html = useMemo(() => (doc ? renderMarkdown(doc.markdown) : ""), [doc])

  if (open) {
    return (
      <article className="space-y-4">
        <button type="button" onClick={() => onOpen()} className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-3.5 w-3.5" aria-hidden />
          {t.allDocuments}
        </button>
        {doc ? (
          <>
            <p className="text-xs text-muted-foreground">{t.updatedOn(formatDay(doc.updated_at, lang))}</p>
            <div
              className="prose prose-sm max-w-none dark:prose-invert"
              // Sanitised by renderMarkdown (DOMPurify) before it gets here.
              dangerouslySetInnerHTML={{ __html: html }}
            />
          </>
        ) : (
          <p className="text-muted-foreground">{t.noDocument}</p>
        )}
      </article>
    )
  }
  return (
    <section aria-label={t.documents}>
      <ul className="divide-y rounded-lg border">
        {documents.map((d) => (
          <li key={d.id}>
            <Link
              to={`?view=documents&doc=${encodeURIComponent(d.id)}`}
              onClick={(e) => {
                e.preventDefault()
                onOpen(d.id)
              }}
              className="flex items-center gap-3 px-3.5 py-3 hover:bg-secondary"
            >
              <FileText className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
              <div className="min-w-0 flex-1">
                <p className="font-medium">{d.title}</p>
                <p className="text-xs text-muted-foreground">{t.updatedOn(formatDay(d.updated_at, lang))}</p>
              </div>
              <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
            </Link>
          </li>
        ))}
      </ul>
    </section>
  )
}
