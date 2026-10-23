import { useEffect, useState, type ReactNode } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Circle, Copy, GitBranch, RefreshCw } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useTunnelCapabilities } from "@/hooks/useTunnelCapabilities"
import { useTunnelStatus } from "@/hooks/useTunnelStatus"
import { startTunnel, stopTunnel } from "@/lib/api"
import { cn } from "@/lib/utils"
import { t, type MessageKey } from "@/i18n"
import type { TunnelCapabilities, TunnelProvider, TunnelProviderId, TunnelState, TunnelStatus } from "@/lib/types"

// The forges a remote names by host, as the design prints them.
const HOST_NAMES: Record<string, string> = { "github.com": "GitHub", "gitlab.com": "GitLab" }

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
  const byId = (id: TunnelProviderId) => providers.find((p) => p.id === id)
  const state: TunnelState = status?.state ?? "idle"
  const up = state === "starting" || state === "running" || state === "stopping"
  const running = up ? providers.find((p) => p.id === status?.provider) : undefined
  const [choice, setChoice] = useState<TunnelProviderId>(() =>
    byId("cloudflared")?.binary.found || !byId("tailscale-serve")?.binary.found ? "cloudflared" : "tailscale-serve",
  )
  // While a share is up, it is the one shown: switching means stopping it first.
  const selected = running ?? byId(choice) ?? providers[0]
  const tailscale = selected?.id !== "cloudflared"
  // Two programs, as the panel offers them; tailscale's two modes are a choice inside its card.
  const cards = [
    { kind: "cloudflared" as const, id: "cloudflared" as TunnelProviderId, name: "share.provider.cloudflared", hint: "share.provider.cloudflared_hint" },
    { kind: "tailscale" as const, id: (tailscale && selected ? selected.id : "tailscale-serve") as TunnelProviderId, name: "share.provider.tailscale", hint: "share.provider.tailscale_hint" },
  ].filter((c) => byId(c.id))

  return (
    <div className="space-y-4 text-[13px]">
      <dl className="grid grid-cols-[5.5rem_minmax(0,1fr)] items-center gap-x-3 gap-y-2.5 border-b pb-4">
        <dt className="text-xs text-muted-foreground">{t("share.repo")}</dt>
        <dd className="flex min-w-0 flex-wrap items-center gap-2">
          {caps.repo?.remote ? (
            <>
              <GitBranch className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
              <span className="truncate font-medium">{caps.repo.slug || caps.repo.remote}</span>
              {caps.repo.branch ? <span className="rounded-full bg-secondary px-2 py-0.5 font-mono text-[11px]">{caps.repo.branch}</span> : null}
              {caps.repo.host ? <span className="text-xs text-muted-foreground">{HOST_NAMES[caps.repo.host] ?? caps.repo.host}</span> : null}
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
              <CopyButton text={caps.local_url} label={t("share.copy_local")} iconOnly />
            </dd>
          </>
        ) : null}
      </dl>

      <fieldset>
        <legend className="sr-only">{t("share.via")}</legend>
        <div className="grid gap-2 sm:grid-cols-2">
          {cards.map((c) => {
            const p = byId(c.id)!
            const checked = c.kind === (tailscale ? "tailscale" : "cloudflared")
            const locked = !!running && !checked
            return (
              <label
                key={c.kind}
                className={cn(
                  "flex cursor-pointer flex-col items-start gap-1 rounded-lg border bg-background p-3 transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring",
                  checked ? "border-brand ring-1 ring-brand" : "hover:bg-secondary",
                  locked && "cursor-not-allowed opacity-50",
                )}
              >
                <input type="radio" name="share-provider" value={c.kind} checked={checked} disabled={locked} onChange={() => setChoice(c.id)} className="sr-only" />
                <span className="font-medium">{t(c.name as MessageKey)}</span>
                <span className="text-xs text-muted-foreground">{t(c.hint as MessageKey)}</span>
                <span className={cn("text-xs", p.binary.found ? "text-status-done" : "text-status-blocked")}>
                  {p.binary.found ? t("share.installed_cmd", { command: p.command }) : t("share.missing")}
                </span>
              </label>
            )
          })}
        </div>
      </fieldset>

      {tailscale && selected?.binary.found ? (
        <div role="radiogroup" aria-label={t("share.mode")} className="inline-flex rounded-md border p-0.5">
          {(["tailscale-serve", "tailscale-funnel"] as const).map((id) => (
            <button
              key={id}
              type="button"
              role="radio"
              aria-checked={selected.id === id}
              disabled={!!running && running.id !== id}
              onClick={() => setChoice(id)}
              className={cn("h-7 rounded px-2.5 text-xs disabled:opacity-50", selected.id === id ? "bg-secondary font-medium" : "text-muted-foreground hover:text-foreground")}
            >
              {t(id === "tailscale-serve" ? "share.mode.serve" : "share.mode.funnel")}
            </button>
          ))}
        </div>
      ) : null}
      {running && running.id !== choice ? (
        <p className="text-xs text-muted-foreground">{t("share.running_other", { provider: providerName(running.id) })}</p>
      ) : null}

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

      <p className="border-t pt-3 text-xs text-muted-foreground">{t("share.footer")}</p>
    </div>
  )
}

function ShareControl({ provider, status }: { provider: TunnelProvider; status: TunnelStatus | undefined }) {
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const state: TunnelState = status?.state ?? "idle"
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
  const stop = (
    <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void act(stopTunnel)} className="border-status-failed/40 text-status-failed hover:text-status-failed">
      {busy ? t("tunnel.stopping") : t("tunnel.stop")}
    </Button>
  )

  return (
    <div className="space-y-3">
      {status?.share_url && status.portal_url ? (
        <div className="space-y-3">
          <p className="flex items-center gap-1.5 text-xs text-status-done">
            <Circle className="h-1.5 w-1.5 fill-current" aria-hidden />
            {t("share.live")}
            {status.started_at ? <Uptime since={status.started_at} /> : null}
          </p>
          <LinkField label={t("share.client_link")} copyLabel={t("tunnel.link.portal_copy")} url={status.portal_url} primary />
          <LinkField label={t("share.operator_link")} copyLabel={t("tunnel.link.dashboard_copy")} url={status.share_url} />
          {stop}
        </div>
      ) : up ? (
        <div className="space-y-2">
          <p className="text-muted-foreground">{status?.url ? t("tunnel.orphan", { url: status.url }) : t("share.waiting")}</p>
          {stop}
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-3 rounded-lg border border-dashed p-3">
          <span className="min-w-0 flex-1">{t("share.idle")}</span>
          <Button type="button" size="sm" disabled={busy || state === "stopping"} onClick={() => void act(() => startTunnel(provider.id))} className="bg-brand text-white hover:bg-brand/90">
            {busy ? t("tunnel.starting") : t("share.generate")}
          </Button>
        </div>
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

/** A program that is missing: this machine's install steps, as one block to copy, and a way back. */
function InstallGuide({ provider, checking, onCheck }: { provider: TunnelProvider; checking: boolean; onCheck: () => void }) {
  const guide = provider.guides.find((g) => g.id === provider.recommended_guide) ?? provider.guides[0]
  return (
    <section
      role="status"
      aria-label={t("share.install_title", { command: provider.command })}
      className="space-y-2.5 rounded-lg border border-status-blocked/40 bg-status-blocked/10 p-3"
    >
      <p className="font-medium text-status-blocked">{t("share.install_title", { command: provider.command })}</p>
      <p>{t("share.install_intro")}</p>
      {guide ? <CommandBlock command={guide.steps.map((s) => s.command).join("\n")} /> : null}
      <div className="flex flex-wrap items-center gap-2">
        <CheckAgain checking={checking} onCheck={onCheck} />
        <a href="/system/tools" className="inline-flex h-7 items-center rounded-md border px-2.5 text-xs hover:bg-secondary">
          {t("share.all_tools")}
        </a>
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

function LinkField({ label, copyLabel, url, primary }: { label: string; copyLabel: string; url: string; primary?: boolean }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="flex items-center gap-2">
        <input readOnly value={url} onFocus={(e) => e.currentTarget.select()} className="h-8 min-w-0 flex-1 rounded-md border bg-muted/40 px-2.5 font-mono text-xs" />
        <CopyButton text={url} label={copyLabel} primary={primary} />
      </span>
    </label>
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

function CopyButton({ text, label, iconOnly, primary }: { text: string; label: string; iconOnly?: boolean; primary?: boolean }) {
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
    <Button
      type="button"
      size="sm"
      variant={primary ? "default" : "outline"}
      onClick={() => void copy()}
      aria-label={label}
      title={iconOnly ? label : undefined}
      className={cn("h-8 shrink-0 gap-1.5", iconOnly ? "w-8 px-0" : "px-3")}
    >
      {iconOnly ? <Copy className="h-3.5 w-3.5" aria-hidden /> : copied ? t("common.copied") : t("common.copy")}
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
