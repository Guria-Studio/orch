import { useState } from "react"
import { AlertTriangle, ExternalLink, RefreshCw } from "lucide-react"
import { CopyCommand } from "@/components/CopyCommand"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useTools, type Tool } from "@/hooks/useTools"
import { t, type MessageKey } from "@/i18n"
import { cn } from "@/lib/utils"

// The design's three groups, in its order: what shares and delivers the
// work (the tunnel programs, git and the PR CLIs), quality, then the agents.
const GROUPS: { key: MessageKey; ids: string[] }[] = [
  { key: "tools.group.share_delivery", ids: ["cloudflared", "tailscale", "git", "gh", "glab"] },
  { key: "tools.group.quality", ids: ["playwright"] },
  { key: "tools.group.agents", ids: ["claude", "codex", "opencode", "gemini", "agy"] },
]

/** Installed, missing (this project uses it), or not used here. */
function stateOf(tool: Tool): { label: MessageKey; tone: string } {
  if (tool.found) return { label: "tools.state.installed", tone: "text-status-done" }
  return tool.used ? { label: "tools.state.missing", tone: "text-status-blocked" } : { label: "tools.state.unused", tone: "text-muted-foreground" }
}

/**
 * System › Tools: the programs orch runs on this machine — what each is for,
 * whether it is here, and, for one this project uses and lacks, how to
 * install it. The same answers as `orch doctor`, as one list; each feature
 * that needs a tool also says so where it is used (the Share panel).
 */
export function ToolsPage() {
  const { data, isLoading, isError, error, isFetching, refetch } = useTools()
  const tools = data?.tools ?? []
  const missing = tools.filter((tool) => tool.used && !tool.found).length

  return (
    <div className="space-y-6 text-[13px]">
      <header className="flex flex-wrap items-center justify-end gap-3">
        <h1 className="sr-only">{t("tools.title")}</h1>
        {data && missing > 0 ? <span className="text-xs text-status-blocked">{t("tools.missing_used", { count: missing })}</span> : null}
        <Button type="button" size="sm" variant="outline" onClick={() => void refetch()} disabled={isFetching}>
          <RefreshCw className={cn("mr-1.5 h-3.5 w-3.5", isFetching && "animate-spin")} aria-hidden />
          {isFetching ? t("tunnel.checking") : t("tunnel.check_again")}
        </Button>
      </header>

      <p className="max-w-2xl leading-5 text-muted-foreground">{t("tools.intro")}</p>

      {isLoading ? <Skeleton className="h-64 w-full" /> : null}
      {isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("tools.failed")}</AlertTitle>
          <AlertDescription>{error?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : null}

      {GROUPS.map((group) => {
        const rows = group.ids.map((id) => tools.find((tool) => tool.id === id)).filter((tool): tool is Tool => !!tool)
        if (rows.length === 0) return null
        return (
          <section key={group.key} aria-labelledby={`tools-${group.key}`} className="space-y-2">
            <h2 id={`tools-${group.key}`} className="text-xs font-medium text-muted-foreground">
              {t(group.key)}
            </h2>
            <ul className="divide-y overflow-x-auto rounded-lg border">
              {rows.map((tool) => (
                <ToolRow key={tool.id} tool={tool} />
              ))}
            </ul>
          </section>
        )
      })}
    </div>
  )
}

function ToolRow({ tool }: { tool: Tool }) {
  const installable = tool.used && !tool.found
  // A tool this project needs and lacks opens on its install steps.
  const [open, setOpen] = useState(installable)
  const state = stateOf(tool)
  return (
    <li className="min-w-[640px]">
      <div className="grid min-h-11 grid-cols-[8.5rem_minmax(0,1fr)_9rem_9.5rem] items-center gap-x-4 px-3.5">
        <span className="font-mono text-xs">{tool.id}</span>
        <span className="text-muted-foreground">{t(`tools.purpose.${tool.id}` as MessageKey)}</span>
        <span className={cn("inline-flex items-center gap-1.5 text-xs", state.tone)} title={tool.version ? `${tool.version} · ${tool.path ?? ""}` : tool.path}>
          <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-current" aria-hidden />
          {t(state.label)}
        </span>
        {installable ? (
          <Button type="button" size="sm" aria-expanded={open} onClick={() => setOpen(!open)} className="h-7 justify-self-end">
            {t("tools.how_to_install")}
          </Button>
        ) : (
          <span />
        )}
      </div>
      {installable && open ? (
        <div className="space-y-2 px-3.5 pb-3.5">
          {tool.install ? <CopyCommand command={tool.install} className="mt-0" /> : <p className="text-xs text-muted-foreground">{t("tools.no_command")}</p>}
          {!tool.install && tool.docs_url ? (
            <a
              href={tool.docs_url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-4 hover:underline"
            >
              {t("tools.docs")}
              <ExternalLink className="h-3 w-3" aria-hidden />
            </a>
          ) : null}
        </div>
      ) : null}
    </li>
  )
}
