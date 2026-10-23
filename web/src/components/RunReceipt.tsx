import { useRef, useState } from "react"
import { Check, ClipboardCopy, Download, ImageIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { receiptImage, receiptMarkdown, type ReceiptPayload } from "@/hooks/useReceipt"
import { fmt, t } from "@/i18n"
import { formatDuration } from "@/lib/time"

const BUILT_WITH_KEY = "orch.receipt.builtWith"

function readBuiltWith(): boolean {
  try {
    return window.localStorage.getItem(BUILT_WITH_KEY) !== "off"
  } catch {
    return true
  }
}

// Copying an image needs ClipboardItem (absent from older Firefox and from
// plain-http pages); the button only shows where it can work.
const canCopyImage = () => typeof ClipboardItem !== "undefined" && typeof navigator.clipboard?.write === "function"

/** The last finished run as label/value rows — tasks, attempts, time, PRs, tokens, cost — and a copy as Markdown or as an image. */
export function RunReceipt({ payload }: { payload: ReceiptPayload }) {
  const r = payload.receipt
  const [builtWith, setBuiltWith] = useState(readBuiltWith)
  const [copied, setCopied] = useState(false)
  const [manual, setManual] = useState(false)
  const [imageCopied, setImageCopied] = useState(false)
  const [imageFailed, setImageFailed] = useState(false)
  const area = useRef<HTMLTextAreaElement>(null)
  if (!r) return null

  const text = receiptMarkdown(payload, builtWith)
  const toggle = (on: boolean) => {
    setBuiltWith(on)
    try {
      window.localStorage.setItem(BUILT_WITH_KEY, on ? "on" : "off")
    } catch {
      // Storage blocked: the choice lasts for this page only.
    }
  }
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      // No clipboard (plain http, or refused): show the text selected instead.
      setManual(true)
      window.setTimeout(() => area.current?.select(), 0)
    }
  }
  const download = async () => {
    setImageFailed(false)
    try {
      const url = URL.createObjectURL(await receiptImage(r.run_id, builtWith))
      const a = document.createElement("a")
      a.href = url
      a.download = `orch-${r.run_id}.png`
      a.click()
      window.setTimeout(() => URL.revokeObjectURL(url), 0)
    } catch {
      setImageFailed(true)
    }
  }
  const copyImage = async () => {
    setImageFailed(false)
    try {
      // The promise, not the blob: Safari only allows the write while the
      // click is still the current gesture.
      await navigator.clipboard.write([new ClipboardItem({ "image/png": receiptImage(r.run_id, builtWith) })])
      setImageCopied(true)
      window.setTimeout(() => setImageCopied(false), 1500)
    } catch {
      setImageFailed(true)
    }
  }
  const prsPassed = r.prs.filter((p) => p.ci_status === "success").length
  const weighted = r.providers.reduce((n, p) => n + p.weighted_tokens, 0)
  const raw = r.providers.reduce((n, p) => n + p.tokens_in + p.tokens_out, 0)
  const priced = r.providers.some((p) => p.cost_source !== "no_data")
  const estimated = r.estimated_cost_usd > 0
  const rows: [string, React.ReactNode][] = [
    [
      t("receipt.row.tasks"),
      <span key="tasks">
        {t("receipt.done", { count: r.done.length })}
        {r.blocked.length > 0 ? <span className="text-status-blocked"> · {t("receipt.blocked", { count: r.blocked.length })}</span> : null}
      </span>,
    ],
    [t("receipt.row.attempts"), t("receipt.failed_attempts", { count: r.failed_attempts })],
    [t("receipt.row.time"), t("receipt.times_short", { wall: formatDuration(r.wall_seconds), agent: formatDuration(r.agent_seconds) })],
    [t("receipt.row.prs"), t("receipt.prs", { count: r.prs.length, passed: prsPassed })],
    [
      t("receipt.row.tokens"),
      raw > 0
        ? weighted !== raw
          ? t("receipt.tokens_weighted", { weighted: fmt.number(weighted), raw: fmt.number(raw) })
          : t("receipt.tokens", { raw: fmt.number(raw) })
        : t("receipt.dispatches", { count: r.dispatches }),
    ],
    [
      t("receipt.row.cost"),
      priced ? (
        <span key="cost">
          <span>{`${estimated ? "~" : ""}${fmt.usd(r.total_cost_usd)}`}</span>
          {estimated ? <span className="ml-1.5 text-muted-foreground">{t("now.cost.estimated")}</span> : null}
        </span>
      ) : (
        <span key="cost" className="text-muted-foreground">
          {t("now.cost.no_data")}
        </span>
      ),
    ],
  ]

  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      <dl className="grid grid-cols-[5.5rem_minmax(0,1fr)] gap-x-3 gap-y-1.5 px-3.5 py-3">
        {rows.map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="min-w-0 tabular-nums">{value}</dd>
          </div>
        ))}
      </dl>

      <div className="flex flex-wrap items-center gap-2 border-t px-3.5 py-3">
        <Button type="button" size="sm" variant="outline" onClick={copy}>
          {copied ? <Check className="h-4 w-4" aria-hidden /> : <ClipboardCopy className="h-4 w-4" aria-hidden />}
          {copied ? t("common.copied") : t("receipt.copy")}
        </Button>
        <Button type="button" size="sm" variant="outline" onClick={download}>
          <Download className="h-4 w-4" aria-hidden />
          {t("receipt.download_image")}
        </Button>
        {canCopyImage() ? (
          <Button type="button" size="sm" variant="ghost" onClick={copyImage}>
            {imageCopied ? <Check className="h-4 w-4" aria-hidden /> : <ImageIcon className="h-4 w-4" aria-hidden />}
            {imageCopied ? t("common.copied") : t("receipt.copy_image")}
          </Button>
        ) : null}
        <label className="ml-auto flex cursor-pointer items-center gap-2 text-xs text-muted-foreground">
          <input
            id="receipt-built-with"
            type="checkbox"
            className="h-3.5 w-3.5 accent-brand"
            checked={builtWith}
            onChange={(e) => toggle(e.target.checked)}
          />
          {t("receipt.built_with")}
        </label>
      </div>
      {imageFailed ? (
        <p role="alert" className="border-t px-3.5 py-2 text-xs text-status-blocked">
          {t("receipt.image_failed")}
        </p>
      ) : null}
      {manual ? (
        <div className="border-t px-3.5 py-3">
          <p className="mb-2 text-xs text-muted-foreground">{t("receipt.copy_manual")}</p>
          <textarea
            ref={area}
            readOnly
            aria-label={t("receipt.copy")}
            value={text}
            className="h-40 w-full resize-y rounded-md border bg-muted p-2 font-mono text-[11px]"
          />
        </div>
      ) : null}
    </div>
  )
}
