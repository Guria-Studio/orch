import { STATUS, statusKey } from "@/lib/status"
import { cn } from "@/lib/utils"

/**
 * The status ring every task row uses, as the design draws it: the status
 * color as a hollow circle — half-open while in progress, filled when done.
 * `label` names it for screen readers where the row does not say it otherwise.
 */
export function StatusRing({ status, label, className }: { status: string; label?: string; className?: string }) {
  const key = statusKey(status)
  return (
    <span
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      className={cn(
        "inline-block h-3 w-3 shrink-0 rounded-full border-2 border-current",
        STATUS[key].text,
        key === "done" && "bg-current",
        key === "in_progress" && "border-r-transparent",
        className,
      )}
    />
  )
}
