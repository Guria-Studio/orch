import { useState } from "react"
import { AlertTriangle, ExternalLink, RefreshCw } from "lucide-react"
import { CopyCommand } from "@/components/CopyCommand"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useTools, type Tool, type ToolGroup } from "@/hooks/useTools"
import { t, type MessageKey } from "@/i18n"
import { cn } from "@/lib/utils"

const GROUPS: ToolGroup[] = ["agents", "repo", "share", "quality"]

/** What a tool's state is, from this project's point of view. */
function stateOf(tool: Tool): { label: MessageKey; tone: string } {
  if (tool.found) return tool.used ? { label: "tools.state.installed", tone: "text-status-done" } : { label: "tools.state.available", tone: "text-muted-foreground" }
  return tool.used ? { label: "tools.state.missing", tone: "text-status-blocked" } : { label: "tools.state.unused", tone: "text-muted-foreground" }
}

/**
 * System › Tools: the programs orch runs on this machine — which this
 * project uses, which are installed, and how to install the rest. The same
 * answers as `orch doctor`, as one list; each feature that needs a tool
 * also says so where it is used (the Share panel, for instance).
 */
export function ToolsPage() {
  const { data, isLoading, isError, error, isFetching, refetch } = useTools()
  const tools = data?.tools ?? []
  const missing = tools.filter((tool) => tool.used && !tool.found).length

  return (
    <div className="max-w-5xl space-y-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("tools.title")}</h1>
          <p className="max-w-2xl text-[13px] text-muted-foreground">{t("tools.intro")}</p>
        </div>
        <div className="flex items-center gap-3">
          {data ? (
            <span className={cn("text-[13px]", missing ? "text-status-blocked" : "text-status-done")}>
              {missing ? t("tools.missing_used", { count: missing }) : t("tools.all_set")}
            </span>
          ) : null}
          <Button type="button" size="sm" variant="outline" onClick={() => void refetch()} disabled={isFetching}>
            <RefreshCw className={cn("mr-1.5 h-3.5 w-3.5", isFetching && "animate-spin")} aria-hidden />
            {isFetching ? t("tunnel.checking") : t("tunnel.check_again")}
          </Button>
        </div>
      </header>

      {isLoading ? <Skeleton className="h-64 w-full" /> : null}
      {isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("tools.failed")}</AlertTitle>
          <AlertDescription>{error?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : null}

      {GROUPS.map((group) => {
        const rows = tools.filter((tool) => tool.group === group)
        if (rows.length === 0) return null
        return (
          <section key={group} aria-labelledby={`tools-${group}`} className="space-y-2">
            <h2 id={`tools-${group}`} className="text-xs font-medium text-muted-foreground">
              {t(`tools.group.${group}` as MessageKey)}
            </h2>
            <ul className="divide-y overflow-hidden rounded-lg border">
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
  // A tool this project needs and lacks opens on its install steps.
  const [open, setOpen] = useState(tool.used && !tool.found)
  const state = stateOf(tool)
  const installable = !tool.found
  return (
    <li className="text-[13px]">
      <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 px-3.5 py-2.5 sm:grid-cols-[8.5rem_minmax(0,1fr)_11rem_auto]">
        <span className="font-mono text-xs">{tool.id}</span>
        <span className="order-3 col-span-2 text-muted-foreground sm:order-none sm:col-span-1">{t(`tools.purpose.${tool.id}` as MessageKey)}</span>
        <span className={cn("inline-flex min-w-0 items-center gap-1.5 text-xs", state.tone)} title={tool.path}>
          <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-current" aria-hidden />
          <span className="truncate">
            {t(state.label)}
            {tool.found && tool.version ? <span className="text-muted-foreground"> · {tool.version}</span> : null}
          </span>
        </span>
        {installable ? (
          <Button type="button" size="sm" variant={tool.used ? "default" : "outline"} aria-expanded={open} onClick={() => setOpen(!open)} className="h-7 justify-self-end">
            {t("tools.how_to_install")}
          </Button>
        ) : (
          <span className="hidden sm:block" />
        )}
      </div>
      {installable && open ? (
        <div className="space-y-2 px-3.5 pb-3">
          {tool.install ? <CopyCommand command={tool.install} className="mt-0" /> : <p className="text-xs text-muted-foreground">{t("tools.no_command")}</p>}
          {tool.docs_url ? (
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
