import { AlertTriangle } from "lucide-react"
import { CopyCommand } from "@/components/CopyCommand"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { useSkills, type SkillState } from "@/hooks/useSystem"
import { t } from "@/i18n"
import { cn } from "@/lib/utils"

const TONE: Record<SkillState, string> = {
  current: "text-status-done",
  missing: "text-muted-foreground",
  different: "text-status-blocked",
}

/**
 * System › Skills: orch's embedded skills and, per agent CLI, whether each is
 * installed as this orch would install it (`orch install-skills --dry-run`'s
 * answer). Installing is the CLI's; the page gives the command.
 */
export function SkillsPage() {
  const { data, isLoading, isError, error } = useSkills()
  const skills = data?.skills ?? []
  const targets = skills[0]?.targets.map((x) => x.target) ?? []
  const command = `orch install-skills ${targets.map((x) => `--target ${x}`).join(" ")}`.trim()

  return (
    <div className="max-w-5xl space-y-6 text-[13px]">
      <header className="space-y-1">
        <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("skills.title")}</h1>
        <p className="max-w-3xl text-muted-foreground">{t("skills.intro")}</p>
      </header>

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
        <>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-[640px]">
              <thead className="border-b text-xs text-muted-foreground">
                <tr>
                  <th className="px-3.5 py-2 text-left font-medium">{t("skills.title")}</th>
                  {targets.map((target) => (
                    <th key={target} className="px-3 py-2 text-left font-mono font-medium">
                      {target}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y">
                {skills.map((skill) => (
                  <tr key={skill.name} className="align-top">
                    <td className="max-w-md px-3.5 py-2.5">
                      <p className="font-mono text-xs font-medium">{skill.name}</p>
                      {skill.description ? <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{skill.description}</p> : null}
                    </td>
                    {skill.targets.map((x) => (
                      <td key={x.target} className={cn("px-3 py-2.5 text-xs", TONE[x.state])} title={x.path}>
                        <span className="inline-flex items-center gap-1.5">
                          <span className="h-1.5 w-1.5 rounded-full bg-current" aria-hidden />
                          {t(`skills.state.${x.state}`)}
                        </span>
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <section className="space-y-1.5">
            <p className="text-xs font-medium">{t("skills.install")}</p>
            <CopyCommand command={command} className="mt-0" />
            <p className="text-xs text-muted-foreground">{t("skills.install_hint")}</p>
          </section>
        </>
      ) : null}
    </div>
  )
}
