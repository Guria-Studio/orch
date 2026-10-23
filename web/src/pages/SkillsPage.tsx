import { useState } from "react"
import { AlertTriangle, Check, Copy } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useSkills, type SkillRow, type SkillState } from "@/hooks/useSystem"
import { t, type MessageKey } from "@/i18n"
import { PIPELINE } from "@/lib/skillsPipeline"
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
 * A second tab, How it works, walks the skills from an idea to a run.
 */
export function SkillsPage() {
  const [tab, setTab] = useState<"installed" | "howto">("installed")
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
        <div role="tablist" aria-label={t("skills.title")} className="mr-auto flex gap-1">
          {(["installed", "howto"] as const).map((x) => (
            <button
              key={x}
              type="button"
              role="tab"
              aria-selected={tab === x}
              onClick={() => setTab(x)}
              className={cn("h-7 rounded px-2.5 text-xs", tab === x ? "bg-secondary font-medium" : "text-muted-foreground hover:text-foreground")}
            >
              {t(`skills.tab.${x}`)}
            </button>
          ))}
        </div>
        {outdated.length > 0 ? (
          <span className="text-xs text-status-blocked">{outdated.map((o) => t("skills.outdated", { count: o.count, target: o.target })).join(" · ")}</span>
        ) : null}
        <CopyButton text={command} />
      </header>

      <p className="max-w-3xl leading-5 text-muted-foreground">{t(tab === "installed" ? "skills.intro" : "skills.howto.intro")}</p>

      {isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("skills.failed")}</AlertTitle>
          <AlertDescription>{error?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : null}

      {isLoading ? (
        <Skeleton className="h-64 w-full" />
      ) : tab === "howto" ? (
        <HowItWorks skills={skills} onSeeInstalled={() => setTab("installed")} />
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

/**
 * The pipeline from an idea to dispatched tasks (lib/skillsPipeline.ts): per
 * step its skills, input, output and command, and which skills no CLI has.
 */
function HowItWorks({ skills, onSeeInstalled }: { skills: SkillRow[]; onSeeInstalled: () => void }) {
  const installedIn = (name: string) =>
    skills.find((s) => s.name === name)?.targets.filter((x) => x.state !== "missing").map((x) => x.target) ?? []
  const code = "rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
  return (
    <div className="max-w-3xl space-y-5">
      <p className="flex flex-wrap items-center gap-1.5 font-mono text-xs" aria-hidden>
        {t("skills.howto.flow")
          .split(" → ")
          .map((x, i) => (
            <span key={x} className="inline-flex items-center gap-1.5">
              {i > 0 ? <span className="text-muted-foreground">→</span> : null}
              <span className="rounded-md border px-2 py-0.5">{x}</span>
            </span>
          ))}
      </p>
      <ol className="space-y-3">
        {PIPELINE.map((step, i) => {
          const missing = step.skills.filter((name) => installedIn(name).length === 0)
          return (
            <li key={step.key} data-step={step.key} className="space-y-2 rounded-lg border p-3.5">
              <h2 className="font-medium">
                <span className="mr-2 text-muted-foreground">{i + 1}.</span>
                {t(`skills.howto.step.${step.key}` as MessageKey)}
              </h2>
              <p className="leading-5 text-muted-foreground">{t(`skills.howto.desc.${step.key}` as MessageKey)}</p>
              <div className="flex flex-wrap gap-1.5">
                {step.skills.map((name) => {
                  const where = installedIn(name)
                  return (
                    <span
                      key={name}
                      title={where.length > 0 ? t("skills.howto.installed_in", { targets: where.join(", ") }) : t("skills.howto.not_installed")}
                      className={cn("rounded-md border px-1.5 py-0.5 font-mono text-xs", where.length > 0 ? "text-status-done" : "text-muted-foreground")}
                    >
                      {name}
                    </span>
                  )
                })}
              </div>
              <dl className="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
                <dt className="text-muted-foreground">{t("skills.howto.reads")}</dt>
                <dd>{step.reads ? <code className={code}>{step.reads}</code> : t("skills.howto.idea")}</dd>
                <dt className="text-muted-foreground">{t("skills.howto.writes")}</dt>
                <dd>{step.writes ? <code className={code}>{step.writes}</code> : t("skills.howto.code")}</dd>
                <dt className="text-muted-foreground">{t("skills.howto.command")}</dt>
                <dd>
                  <code className={code}>{step.command}</code>
                </dd>
              </dl>
              {missing.length > 0 ? (
                <p className="flex flex-wrap items-center gap-1.5 text-xs text-status-blocked">
                  {t("skills.howto.missing", { skills: missing.join(", ") })} <code className={code}>orch install-skills</code>
                  <Button type="button" variant="link" size="sm" className="h-auto p-0 text-xs" onClick={onSeeInstalled}>
                    {t("skills.howto.see_installed")}
                  </Button>
                </p>
              ) : null}
            </li>
          )
        })}
      </ol>
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
