import { SharePanel } from "@/components/SharePanel"
import { t } from "@/i18n"

/** The Share panel as a page, for links and bookmarks to /delivery/share; the sidebar opens it as a dialog. */
export function TunnelPage() {
  return (
    <div className="max-w-3xl space-y-6">
      <header className="space-y-1">
        <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("share.title")}</h1>
        <p className="text-[13px] text-muted-foreground">{t("share.intro")}</p>
      </header>
      <SharePanel />
    </div>
  )
}
