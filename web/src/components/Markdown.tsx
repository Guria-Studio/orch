import { useEffect, useMemo, useRef } from "react"
import { t } from "@/i18n"
import { cn } from "@/lib/utils"
import { renderMarkdown } from "@/stakeholder/portal"

const bar = "flex items-center justify-between gap-2 border-b px-3 py-1 font-sans text-[11px] text-muted-foreground"
const button = "rounded px-1.5 py-0.5 hover:bg-secondary hover:text-foreground"

/**
 * Agent- or GitHub-written markdown, sanitized (renderMarkdown). Each fenced
 * block gets a bar with its language and a copy button; a block that names a
 * language is colored by lib/highlight, imported only when there is one.
 */
export function Markdown({ source, breaks = false, className }: { source: string; breaks?: boolean; className?: string }) {
  const html = useMemo(() => renderMarkdown(source, { breaks }), [source, breaks])
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const tagged: [HTMLElement, string][] = []
    const heads: HTMLElement[] = []
    for (const code of ref.current?.querySelectorAll<HTMLElement>("pre > code") ?? []) {
      const pre = code.parentElement!
      const lang = /\blanguage-(\S+)/.exec(code.className)?.[1] ?? ""
      if (lang) tagged.push([code, lang])
      const head = document.createElement("div")
      head.className = bar
      const label = document.createElement("span")
      label.className = "font-mono"
      label.textContent = lang
      const copy = document.createElement("button")
      copy.type = "button"
      copy.className = button
      copy.textContent = t("common.copy")
      copy.onclick = () => {
        navigator.clipboard?.writeText(code.textContent ?? "").then(
          () => {
            copy.textContent = t("common.copied")
            window.setTimeout(() => (copy.textContent = t("common.copy")), 1500)
          },
          () => {}, // clipboard refused (insecure origin): the text is still selectable
        )
      }
      head.append(label, copy)
      pre.prepend(head)
      heads.push(head)
    }
    if (tagged.length > 0) void import("@/lib/highlight").then(({ highlight }) => tagged.forEach(([code, lang]) => highlight(code, lang)))
    return () => heads.forEach((head) => head.remove())
  }, [html])

  return (
    <div
      ref={ref}
      className={cn(
        "prose prose-sm dark:prose-invert max-w-none break-words",
        "prose-code:before:content-none prose-code:after:content-none prose-headings:text-sm [&_a]:text-brand [&_li]:my-1 [&_ul]:list-disc [&_ul]:pl-5",
        "[&_:not(pre)>code]:rounded [&_:not(pre)>code]:bg-muted [&_:not(pre)>code]:px-1 [&_:not(pre)>code]:py-0.5 [&_:not(pre)>code]:font-normal",
        "[&_pre]:overflow-hidden [&_pre]:border [&_pre]:bg-muted/60 [&_pre]:p-0 [&_pre]:text-foreground [&_pre>code]:block [&_pre>code]:overflow-x-auto [&_pre>code]:p-3",
        className,
      )}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}
