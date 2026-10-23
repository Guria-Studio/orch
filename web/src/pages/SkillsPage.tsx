import { useState } from "react"
import { AlertTriangle, Check, Copy } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useSkills, type SkillState } from "@/hooks/useSystem"
import { t } from "@/i18n"
import { cn } from "@/lib/utils"

// The design's columns: the CLIs that read the skills.
const TARGETS = ["claude", "codex", "opencode"]

const TONE: Record<SkillState, string> = {
  current: "text-status-done",
  missing: "text-muted-foreground",
  different: "text-status-blocked",
}

/**
 * System › Skills: orch's embedded skills, when each is used, and whether it
 * is installed in claude, codex and opencode as this orch would install it
 * (`orch install-skills --dry-run`'s answer). The dashboard writes nothing:
 * the header button copies the command that installs or updates them all.
 */
export function SkillsPage() {
  const { data, isLoading, isError, error } = useSkills()
  const skills = data?.skills ?? []
  const command = `orch install-skills ${TARGETS.map((x) => `--target ${x}`).join(" ")} --force`
  const outdated = TARGETS.map((target) => ({
    target,
    count: skills.filter((s) => s.targets.some((x) => x.target === target && x.state === "different")).length,
  })).filter((o) => o.count > 0)

  return (
    <div className="space-y-5 text-[13px]">
      <header className="flex flex-wrap items-center justify-end gap-3">
        <h1 className="sr-only">{t("skills.title")}</h1>
        {outdated.length > 0 ? (
          <span className="text-xs text-status-blocked">{outdated.map((o) => t("skills.outdated", { count: o.count, target: o.target })).join(" · ")}</span>
        ) : null}
        <CopyButton text={command} />
      </header>

      <p className="max-w-3xl leading-5 text-muted-foreground">{t("skills.intro")}</p>

      {isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("skills.failed")}</AlertTitle>
          <AlertDescription>{error?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : null}

      {isLoading ? (
        <Skeleton className="h-64 w-full" />
      ) : skills.length > 0 ? (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full min-w-[720px]">
            <thead className="border-b text-xs text-muted-foreground">
              <tr>
                <th className="w-36 px-3.5 py-2 text-left font-medium">{t("skills.col.skill")}</th>
                <th className="px-3.5 py-2 text-left font-medium">{t("skills.col.when")}</th>
                {TARGETS.map((target) => (
                  <th key={target} className="w-28 px-3 py-2 text-left font-mono font-medium">
                    {target}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y">
              {skills.map((skill) => (
                <tr key={skill.name}>
                  <td className="px-3.5 py-2.5 font-mono text-xs font-medium">{skill.name}</td>
                  <td className="px-3.5 py-2.5 text-muted-foreground">
                    <span className="line-clamp-2">{skill.description}</span>
                  </td>
                  {TARGETS.map((target) => {
                    const x = skill.targets.find((s) => s.target === target)
                    return (
                      <td key={target} className={cn("px-3 py-2.5 text-xs", x ? TONE[x.state] : "text-muted-foreground")} title={x?.path}>
                        {x ? (
                          <span className="inline-flex items-center gap-1.5">
                            <span className="h-1.5 w-1.5 rounded-full bg-current" aria-hidden />
                            {t(`skills.state.${x.state}`)}
                          </span>
                        ) : (
                          "—"
                        )}
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </div>
  )
}

/** Copies the install command; the person runs it in a terminal. */
function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard refused (insecure origin): the title still shows the command.
    }
  }
  return (
    <Button type="button" size="sm" onClick={() => void copy()} title={text}>
      {copied ? <Check className="h-3.5 w-3.5" aria-hidden /> : <Copy className="h-3.5 w-3.5" aria-hidden />}
      {copied ? t("common.copied") : t("skills.copy_install")}
    </Button>
  )
}
