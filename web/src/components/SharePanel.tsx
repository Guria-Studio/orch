import { useEffect, useState, type ReactNode } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Circle, Copy, ExternalLink, GitBranch, RefreshCw } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useTunnelCapabilities } from "@/hooks/useTunnelCapabilities"
import { useTunnelStatus } from "@/hooks/useTunnelStatus"
import { startTunnel, stopTunnel } from "@/lib/api"
import { cn } from "@/lib/utils"
import { t, type MessageKey } from "@/i18n"
import type { TunnelCapabilities, TunnelProvider, TunnelProviderId, TunnelState, TunnelStatus } from "@/lib/types"

type BadgeVariant = "success" | "warning" | "danger" | "muted" | "info"

// The dot takes the badge's own status color, so the pair can never disagree.
const STATE_META: Record<TunnelState, { label: MessageKey; badge: BadgeVariant }> = {
  idle: { label: "tunnel.state.idle", badge: "muted" },
  starting: { label: "tunnel.state.starting", badge: "info" },
  running: { label: "tunnel.state.running", badge: "success" },
  stopping: { label: "tunnel.state.stopping", badge: "warning" },
  error: { label: "tunnel.state.error", badge: "danger" },
}

const providerName = (id: TunnelProviderId) => t(`share.provider.${id}` as MessageKey)

/**
 * Share this dashboard: the repository and local address it serves, a choice
 * of program (a Cloudflare quick tunnel, or tailscale serve/funnel), the
 * install steps for one that is missing, and the two links once it is up —
 * one for a client, one for the operator.
 *
 * The server is the authority on every gate (internal/dashboard/tunnelroutes.go);
 * this panel only explains them. Used by the sidebar's Share dialog and the
 * /delivery/share page.
 */
export function SharePanel() {
  const caps = useTunnelCapabilities()
  // Status answers whenever the request gates pass, even with no program installed yet.
  const gatesPass = caps.data?.reason === "ok" || caps.data?.reason === "binary_missing"
  const { data: status } = useTunnelStatus({ enabled: gatesPass })

  if (caps.isLoading) return <Skeleton className="h-48 w-full" />
  if (caps.isError || !caps.data) {
    return (
      <Alert variant="destructive">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("tunnel.caps_failed")}</AlertTitle>
        <AlertDescription>{caps.error?.message ?? t("common.unknown_error")}</AlertDescription>
      </Alert>
    )
  }
  switch (caps.data.reason) {
    case "config_disabled":
      return (
        <Notice title={t("tunnel.disabled_title")}>
          <p>
            {t("tunnel.disabled_before")} <code className="font-mono text-xs">.orchestrator/config.yaml</code>
            {t("tunnel.disabled_middle")} <code className="font-mono text-xs">orch dashboard</code>:
          </p>
          <CommandBlock command={"tunnel:\n  enabled: true"} />
        </Notice>
      )
    case "profile_gate":
      return (
        <Notice title={t("tunnel.profile_title")}>
          {t("tunnel.profile_before")} (<code className="font-mono text-xs">orch dashboard</code> {t("tunnel.profile_without")}{" "}
          <code className="font-mono text-xs">--profile</code>).
        </Notice>
      )
    case "host_gate":
      return <Notice title={t("tunnel.host_title")}>{t("tunnel.host_detail")}</Notice>
    default:
      return <ShareBody caps={caps.data} status={status} checking={caps.isFetching} onCheck={() => void caps.refetch()} />
  }
}

function ShareBody({
  caps,
  status,
  checking,
  onCheck,
}: {
  caps: TunnelCapabilities
  status: TunnelStatus | undefined
  checking: boolean
  onCheck: () => void
}) {
  const providers = caps.providers ?? []
  const state: TunnelState = status?.state ?? "idle"
  const up = state === "starting" || state === "running" || state === "stopping"
  const running = up ? providers.find((p) => p.id === status?.provider) : undefined
  const [choice, setChoice] = useState<TunnelProviderId>(
    () => providers.find((p) => p.binary.found)?.id ?? providers[0]?.id ?? "cloudflared",
  )
  // While a share is up, it is the one shown: switching means stopping it first.
  const selected = running ?? providers.find((p) => p.id === choice) ?? providers[0]

  return (
    <div className="space-y-5 text-[13px]">
      <dl className="grid grid-cols-[6.5rem_minmax(0,1fr)] items-center gap-x-3 gap-y-2.5">
        <dt className="text-xs text-muted-foreground">{t("share.repo")}</dt>
        <dd className="flex min-w-0 flex-wrap items-center gap-2">
          {caps.repo?.remote ? (
            <>
              <GitBranch className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
              <span className="truncate font-medium">{caps.repo.slug || caps.repo.remote}</span>
              {caps.repo.branch ? (
                <span className="rounded-full bg-secondary px-2 py-0.5 font-mono text-[11px]">{caps.repo.branch}</span>
              ) : null}
              {caps.repo.host ? <span className="text-xs text-muted-foreground">{caps.repo.host}</span> : null}
            </>
          ) : (
            <span className="text-muted-foreground">{t("share.no_repo")}</span>
          )}
        </dd>
        {caps.local_url ? (
          <>
            <dt className="text-xs text-muted-foreground">{t("share.local")}</dt>
            <dd className="flex min-w-0 items-center gap-2">
              <code className="min-w-0 flex-1 truncate rounded-md border bg-muted/40 px-2.5 py-1.5 font-mono text-xs">{caps.local_url}</code>
              <CopyButton text={caps.local_url} label={t("share.copy_local")} />
            </dd>
          </>
        ) : null}
      </dl>

      <fieldset className="space-y-2">
        <legend className="mb-2 text-xs text-muted-foreground">{t("share.via")}</legend>
        <div className="grid gap-2 sm:grid-cols-3">
          {providers.map((p) => {
            const checked = p.id === selected?.id
            return (
              <label
                key={p.id}
                className={cn(
                  "flex cursor-pointer flex-col gap-1 rounded-lg border p-3 transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring",
                  checked ? "border-brand bg-brand/5" : "hover:bg-secondary",
                  running && !checked && "cursor-not-allowed opacity-50",
                )}
              >
                <input
                  type="radio"
                  name="share-provider"
                  value={p.id}
                  checked={checked}
                  disabled={!!running && !checked}
                  onChange={() => setChoice(p.id)}
                  className="sr-only"
                />
                <span className="font-medium">{providerName(p.id)}</span>
                <span className="text-xs text-muted-foreground">{t(`share.provider.${p.id}_hint` as MessageKey)}</span>
                <span className={cn("text-xs", p.binary.found ? "text-status-done" : "text-status-blocked")}>
                  {p.binary.found ? t("share.installed") : t("share.missing")}
                </span>
              </label>
            )
          })}
        </div>
        {running && running.id !== choice ? (
          <p className="text-xs text-muted-foreground">{t("share.running_other", { provider: providerName(running.id) })}</p>
        ) : null}
      </fieldset>

      {selected ? (
        !selected.binary.found ? (
          <InstallGuide provider={selected} checking={checking} onCheck={onCheck} />
        ) : selected.blocker && !up ? (
          <Alert className="border-status-blocked/40 bg-status-blocked/10 [&>svg]:text-status-blocked">
            <AlertTriangle className="h-4 w-4" />
            <AlertTitle>{t("tunnel.blocker_title")}</AlertTitle>
            <AlertDescription className="space-y-2">
              <p>{selected.blocker}</p>
              <CheckAgain checking={checking} onCheck={onCheck} />
            </AlertDescription>
          </Alert>
        ) : (
          <ShareControl provider={selected} status={status} />
        )
      ) : null}

      {selected?.id === "tailscale-funnel" ? <p className="text-xs text-muted-foreground">{t("share.funnel_note")}</p> : null}

      <details className="text-xs text-muted-foreground">
        <summary className="cursor-pointer select-none">{t("tunnel.limits_title")}</summary>
        <ul className="mt-2 list-disc space-y-1 pl-5">
          {(["tunnel.limit.address", "tunnel.limit.revoke", "tunnel.limit.polling", "tunnel.limit.testing"] as const).map((key) => (
            <li key={key}>{t(key)}</li>
          ))}
        </ul>
      </details>
    </div>
  )
}

function ShareControl({ provider, status }: { provider: TunnelProvider; status: TunnelStatus | undefined }) {
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const state: TunnelState = status?.state ?? "idle"
  const meta = STATE_META[state] ?? STATE_META.idle
  const up = state === "starting" || state === "running"

  const act = async (action: () => Promise<TunnelStatus>) => {
    setBusy(true)
    setActionError(null)
    try {
      queryClient.setQueryData(["tunnel-status"], await action())
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
      void queryClient.invalidateQueries({ queryKey: ["tunnel-capabilities"] })
    } finally {
      setBusy(false)
    }
  }

  const problem = actionError ?? (state === "error" || status?.last_error ? status?.last_error ?? null : null)

  return (
    <div className="space-y-3 rounded-lg border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={meta.badge} className="gap-1.5">
          <Circle className="h-2 w-2 fill-current" aria-hidden />
          {t(meta.label)}
        </Badge>
        {state === "running" && status?.started_at ? <Uptime since={status.started_at} /> : null}
        {provider.binary.version ? (
          <span className="truncate font-mono text-[11px] text-muted-foreground">{provider.binary.version}</span>
        ) : null}
        <span className="ml-auto">
          {up ? (
            <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void act(stopTunnel)}>
              {busy ? t("tunnel.stopping") : t("tunnel.stop")}
            </Button>
          ) : (
            <Button type="button" size="sm" disabled={busy || state === "stopping"} onClick={() => void act(() => startTunnel(provider.id))}>
              {busy ? t("tunnel.starting") : t("share.generate")}
            </Button>
          )}
        </span>
      </div>

      {status?.share_url && status.portal_url ? (
        <div className="space-y-2">
          <LinkRow label={t("tunnel.link.portal")} hint={t("tunnel.link.portal_hint")} copyLabel={t("tunnel.link.portal_copy")} url={status.portal_url} />
          <LinkRow
            label={t("tunnel.link.dashboard")}
            hint={t("tunnel.link.dashboard_hint")}
            copyLabel={t("tunnel.link.dashboard_copy")}
            url={status.share_url}
          />
        </div>
      ) : up && status?.url ? (
        <p className="text-muted-foreground">{t("tunnel.orphan", { url: status.url })}</p>
      ) : up ? (
        <p className="text-muted-foreground">{t("share.waiting")}</p>
      ) : (
        <p className="text-muted-foreground">{t("tunnel.two_links")}</p>
      )}

      {problem ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{actionError ? t(up ? "tunnel.did_not_stop" : "tunnel.did_not_start") : t("share.output", { command: provider.command })}</AlertTitle>
          <AlertDescription className="font-mono text-xs">{problem}</AlertDescription>
        </Alert>
      ) : null}
      {status?.log_tail?.length && (problem || (up && !status.url)) ? (
        <pre aria-label={t("share.output", { command: provider.command })} className="max-h-40 overflow-auto rounded-md bg-muted/40 p-2 font-mono text-[11px] leading-4">
          {status.log_tail.join("\n")}
        </pre>
      ) : null}
    </div>
  )
}

function InstallGuide({ provider, checking, onCheck }: { provider: TunnelProvider; checking: boolean; onCheck: () => void }) {
  const [selected, setSelected] = useState(provider.recommended_guide || provider.guides[0]?.id)
  const guide = provider.guides.find((g) => g.id === selected) ?? provider.guides[0]
  return (
    <section className="space-y-4 rounded-lg border border-status-blocked/40 bg-status-blocked/5 p-3" aria-label={t("share.install_title", { command: provider.command })}>
      <div className="space-y-1">
        <p className="font-medium">{t("share.install_title", { command: provider.command })}</p>
        <p className="text-xs text-muted-foreground">{t("share.install_intro")}</p>
      </div>
      <div className="flex flex-wrap gap-1.5" role="group" aria-label={t("tunnel.install_method")}>
        {provider.guides.map((g) => (
          <Button
            key={g.id}
            type="button"
            size="sm"
            variant={g.id === guide?.id ? "default" : "outline"}
            aria-pressed={g.id === guide?.id}
            onClick={() => setSelected(g.id)}
          >
            {g.label}
            {g.id === provider.recommended_guide ? ` · ${t("tunnel.this_machine")}` : ""}
          </Button>
        ))}
      </div>
      {guide ? (
        <ol className="space-y-3">
          {guide.steps.map((step, i) => (
            <li key={step.title} className="grid grid-cols-[1.5rem_minmax(0,1fr)] gap-2">
              <span className="flex h-5 w-5 items-center justify-center rounded-full border text-[11px] font-medium tabular-nums">{i + 1}</span>
              <div className="min-w-0 space-y-1">
                <p className="text-xs font-medium">{step.title}</p>
                <CommandBlock command={step.command} />
              </div>
            </li>
          ))}
        </ol>
      ) : null}
      <div className="flex flex-wrap items-center gap-3">
        <CheckAgain checking={checking} onCheck={onCheck} />
        {provider.docs_url ? (
          <a
            href={provider.docs_url}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-4 hover:underline"
          >
            {t("share.other_systems")}
            <ExternalLink className="h-3 w-3" aria-hidden />
          </a>
        ) : null}
      </div>
    </section>
  )
}

function CheckAgain({ checking, onCheck }: { checking: boolean; onCheck: () => void }) {
  return (
    <Button type="button" size="sm" variant="outline" onClick={onCheck} disabled={checking}>
      <RefreshCw className={cn("mr-1.5 h-3.5 w-3.5", checking && "animate-spin")} aria-hidden />
      {checking ? t("tunnel.checking") : t("tunnel.check_again")}
    </Button>
  )
}

function Notice({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Alert>
      <AlertTriangle className="h-4 w-4" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription className="space-y-2">{children}</AlertDescription>
    </Alert>
  )
}

function LinkRow({ label, hint, copyLabel, url }: { label: string; hint: string; copyLabel: string; url: string }) {
  return (
    <div className="rounded-md border px-3 py-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{label}</span>
        <span className="text-xs text-muted-foreground">{hint}</span>
      </div>
      <div className="mt-1.5 flex items-center gap-2">
        <a href={url} target="_blank" rel="noopener noreferrer" className="flex min-w-0 items-center gap-1.5 font-mono text-xs hover:underline">
          <ExternalLink className="h-3.5 w-3.5 shrink-0" aria-hidden />
          <span className="truncate">{url}</span>
        </a>
        <CopyButton text={url} label={copyLabel} />
      </div>
    </div>
  )
}

function CommandBlock({ command }: { command: string }) {
  return (
    <div className="flex items-start gap-2 rounded-md border bg-muted/40 px-2.5 py-1.5">
      <pre className="min-w-0 flex-1 overflow-x-auto whitespace-pre font-mono text-xs leading-5">{command}</pre>
      <CopyButton text={command} label={t("tunnel.copy_command")} />
    </div>
  )
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard refused (permission / non-secure context): the text is
      // still on screen for a manual copy.
    }
  }
  return (
    <Button type="button" size="sm" variant="outline" onClick={() => void copy()} aria-label={label} className="ml-auto h-7 shrink-0 gap-1.5 px-2">
      <Copy className="h-3.5 w-3.5" aria-hidden />
      {copied ? t("common.copied") : t("common.copy")}
    </Button>
  )
}

function Uptime({ since }: { since: string }) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 30_000)
    return () => window.clearInterval(id)
  }, [])
  return <span className="text-xs text-muted-foreground tabular-nums">{t("tunnel.uptime", { duration: formatUptime(Date.parse(since), now) })}</span>
}

/** "< 1 min", "12 min", "1 h 5 min". */
function formatUptime(startMs: number, nowMs: number): string {
  const minutes = Math.floor(Math.max(0, nowMs - startMs) / 60_000)
  if (!Number.isFinite(minutes) || minutes < 1) return t("tunnel.under_minute")
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  return h > 0 ? t("tunnel.hours_minutes", { h, m }) : t("tunnel.minutes", { m })
}
