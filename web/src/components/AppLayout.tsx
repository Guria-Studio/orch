import { useEffect, useState, type ReactNode } from "react"
import { Navigate, NavLink, useLocation, useNavigate, useSearchParams } from "react-router-dom"
import { Languages, LogOut, Monitor, Moon, MoreHorizontal, PanelLeft, ScrollText, Search, Share, Sun, X } from "lucide-react"
import { CommandPalette, ShortcutsDialog } from "@/components/CommandPalette"
import { SharePanel } from "@/components/SharePanel"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Tooltip } from "@/components/ui/tooltip"
import { LANGS, LANGUAGE_NAMES, setLanguage, t, useLanguage } from "@/i18n"
import { useAuth } from "@/hooks/useAuth"
import { isPortfolioNavVisible, usePortfolio } from "@/hooks/usePortfolio"
import { useSidebarCollapsed } from "@/hooks/useSidebarCollapsed"
import { useWhoami } from "@/hooks/useWhoami"
import { releasesSeen, useReleases } from "@/hooks/useReleases"
import { TAB_ICONS, findTab, homePath, isPathAllowed, visibleDestinations, type NavDestination } from "@/lib/nav"
import { isMac, useKeyboardShortcuts } from "@/lib/shortcuts"
import { nextTheme, useTheme } from "@/lib/theme"
import { cn } from "@/lib/utils"
import { LogsPage } from "@/pages/LogsPage"

interface AppLayoutProps {
  children: ReactNode
}

const THEME_ICON = { dark: Moon, light: Sun, system: Monitor } as const
// The phone's bottom bar: the visible destinations plus "More".
const BOTTOM_COLS: Record<number, string> = { 2: "grid-cols-2", 3: "grid-cols-3", 4: "grid-cols-4", 5: "grid-cols-5" }
const SHARE_PATH = "/delivery/share"

const focusRing = "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
// One row of the sidebar, Linear-sized: 32px tall, 13px text.
const sideRow = cn(
  "flex h-8 w-full items-center rounded-md text-[13px] text-sidebar-muted transition-colors hover:bg-sidebar-active hover:text-sidebar-foreground",
  focusRing,
)

/** A sidebar row as is, or with a tooltip naming it when the sidebar shows icons only. */
function compactable(compact: boolean, label: string, row: ReactNode) {
  return compact ? <Tooltip content={label}>{row}</Tooltip> : row
}

function ThemeToggle({ compact }: { compact: boolean }) {
  const [choice, setChoice] = useTheme()
  const next = nextTheme(choice)
  const Icon = THEME_ICON[choice]
  const label = t("theme.current", { theme: t(`theme.${choice}` as const) })
  return compactable(
    compact,
    label,
    <button
      type="button"
      onClick={() => setChoice(next)}
      aria-label={`${label}. ${t("theme.switch_to", { theme: t(`theme.${next}` as const) })}`}
      className={cn(sideRow, compact ? "justify-center" : "gap-2.5 px-2.5")}
    >
      <Icon className="h-4 w-4 shrink-0" aria-hidden />
      {compact ? null : <span>{t(`theme.${choice}` as const)}</span>}
    </button>,
  )
}

/** Cycles en → es → pt, like the theme toggle; each language is named in itself. */
function LanguageToggle({ compact }: { compact: boolean }) {
  const lang = useLanguage()
  const next = LANGS[(LANGS.indexOf(lang) + 1) % LANGS.length]
  const label = t("language.current", { language: LANGUAGE_NAMES[lang] })
  return compactable(
    compact,
    label,
    <button
      type="button"
      onClick={() => setLanguage(next)}
      aria-label={`${label}. ${t("language.switch_to", { language: LANGUAGE_NAMES[next] })}`}
      className={cn(sideRow, compact ? "justify-center" : "gap-2.5 px-2.5")}
    >
      <Languages className="h-4 w-4 shrink-0" aria-hidden />
      {compact ? null : <span>{LANGUAGE_NAMES[lang]}</span>}
    </button>,
  )
}

/** The destinations. Each link opens the destination's first page this session may see. */
function DestinationLinks({ destinations, currentId, collapsed }: { destinations: NavDestination[]; currentId?: string; collapsed: boolean }) {
  return (
    <>
      {destinations.map((d) => {
        const Icon = d.icon
        const active = d.id === currentId
        return (
          <div key={d.id}>
            {compactable(
              collapsed,
              t(d.label),
              <NavLink
                to={d.tabs[0].to}
                aria-current={active ? "page" : undefined}
                aria-label={collapsed ? t(d.label) : undefined}
                className={cn(
                  sideRow,
                  collapsed ? "justify-center" : "gap-2.5 px-2.5",
                  active && "bg-sidebar-active font-medium text-sidebar-foreground",
                )}
              >
                <Icon className={cn("h-4 w-4 shrink-0", active && "text-brand")} aria-hidden />
                {collapsed ? null : <span>{t(d.label)}</span>}
              </NavLink>,
            )}
          </div>
        )
      })}
    </>
  )
}

/**
 * Whether the running version's notes are still unread here: the What's new
 * tab carries a dot until they are opened.
 */
function useUnseenRelease(enabled: boolean): boolean {
  const { data } = useReleases(enabled)
  const version = data?.releases.some((r) => r.current) ? data.current : ""
  return version !== "" && !releasesSeen(version)
}

/**
 * The page header: where you are (destination › page) and, when the
 * destination has more than one page, its pages as view tabs.
 */
function PageHeader({ destination, pathname }: { destination?: NavDestination; pathname: string }) {
  const { search } = useLocation()
  const unseen = useUnseenRelease(destination?.id === "now")
  if (!destination) return null
  const currentTab = findTab(pathname)?.tab
  const tabs = destination.tabs.filter((tab) => !tab.offTabs)
  // Filters live in the query string; moving between List, Kanban and Graph keeps them.
  const carry = new URLSearchParams(search)
  carry.delete("logs")
  const qs = carry.toString()
  return (
    <div className="sticky top-14 z-20 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80 md:top-0">
      <div className="flex min-h-12 items-center gap-3 overflow-x-auto px-4 md:px-6">
        <p className="hidden shrink-0 items-center gap-2 text-[13px] md:flex">
          <span className="text-muted-foreground">{t(destination.label)}</span>
          {currentTab && (tabs.length > 1 || currentTab.offTabs) ? (
            <>
              <span className="text-muted-foreground" aria-hidden>
                ›
              </span>
              <span className="font-medium">{t(currentTab.label)}</span>
            </>
          ) : null}
        </p>
        {tabs.length > 1 ? (
          <nav aria-label={t("nav.sections")} className="flex min-w-max gap-1 md:ml-2">
            {tabs.map((tab) => {
              const Icon = TAB_ICONS[tab.to]
              const active = currentTab?.to === tab.to
              return (
                <NavLink
                  key={tab.to}
                  to={`${tab.to}${qs ? `?${qs}` : ""}`}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-[13px] transition-colors",
                    focusRing,
                    active
                      ? "border-input bg-secondary font-medium text-foreground"
                      : "border-transparent text-muted-foreground hover:bg-secondary hover:text-foreground",
                  )}
                >
                  {Icon ? <Icon className="hidden h-3.5 w-3.5 shrink-0 sm:block" aria-hidden /> : null}
                  {t(tab.label)}
                  {tab.to === "/now/releases" && unseen && !active ? (
                    <span className="h-1.5 w-1.5 rounded-full bg-brand" role="img" aria-label={t("releases.unseen")} />
                  ) : null}
                </NavLink>
              )
            })}
          </nav>
        ) : null}
      </div>
    </div>
  )
}

/**
 * The event log as a panel over whatever page is open, so following a run
 * never means leaving the page that raised the question. `?logs=1` opens it,
 * which is also what the old /logs address redirects to.
 */
function LogsDrawer({ onClose, collapsed }: { onClose: () => void; collapsed: boolean }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [onClose])
  return (
    <section
      aria-label={t("nav.logs")}
      className={cn(
        "fixed inset-x-0 bottom-16 z-40 h-[62vh] border-t bg-background shadow-[0_-12px_32px_-12px_rgb(0_0_0/0.35)] md:bottom-0",
        collapsed ? "md:left-14" : "md:left-60",
      )}
    >
      <button
        type="button"
        onClick={onClose}
        aria-label={t("nav.close_logs")}
        className={cn(
          "absolute right-3 top-3 z-10 flex h-9 w-9 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground",
          focusRing,
        )}
      >
        <X className="h-4 w-4" aria-hidden />
      </button>
      {/* LogsPage sizes itself for a full page; inside the panel it fills the panel instead. */}
      <div className="h-full pb-4 pl-4 pr-14 pt-4 md:pl-6 [&>div]:h-full">
        <LogsPage />
      </div>
    </section>
  )
}

export function AppLayout({ children }: AppLayoutProps) {
  const { clearToken } = useAuth()
  const navigate = useNavigate()
  const [collapsed, , toggle] = useSidebarCollapsed()
  const { data: whoami } = useWhoami()
  const { pathname } = useLocation()
  const [params, setParams] = useSearchParams()
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [shortcutsOpen, setShortcutsOpen] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const [shareOpen, setShareOpen] = useState(false)

  const handleLogout = () => {
    clearToken()
    navigate("/login", { replace: true })
  }

  // Sprint E-6: fail-open. If /api/whoami hasn't answered yet (undefined) OR
  // errored out, assume operator so we never hide UI from an operator who
  // hit a transient hiccup. Stakeholder gating only kicks in on an EXPLICIT
  // "stakeholder" answer, and then follows the allow-list the server sent.
  const isStakeholder = whoami?.profile === "stakeholder"
  const allowedRoutes = whoami?.routes

  // G8.5: never even requested for a stakeholder session — portfolio data
  // (other projects' names, paths, spend) is operator-only.
  const { data: portfolioData, error: portfolioError } = usePortfolio({
    enabled: !isStakeholder,
  })
  const portfolioAvailable = isPortfolioNavVisible({
    isStakeholder,
    data: portfolioData,
    error: portfolioError,
  })

  const destinations = visibleDestinations({ isStakeholder, allowedRoutes, portfolioAvailable })
  const currentId = findTab(pathname)?.destination.id
  const current = destinations.find((d) => d.id === currentId)
  const canSeeLogs = !isStakeholder || (allowedRoutes ? allowedRoutes.includes("api_events") : false)
  const canShare = destinations.some((d) => d.tabs.some((tab) => tab.to === SHARE_PATH))
  // The System group is the operator's; a stakeholder allowed the log sees it among the controls.
  const showSystem = canSeeLogs && !isStakeholder
  const logsOpen = canSeeLogs && params.get("logs") === "1"

  const setLogs = (open: boolean) => {
    const next = new URLSearchParams(params)
    if (open) next.set("logs", "1")
    else next.delete("logs")
    setParams(next, { replace: true })
  }

  useKeyboardShortcuts({
    destinations,
    onPalette: () => setPaletteOpen((open) => !open),
    onShortcuts: () => setShortcutsOpen(true),
    onToggleSidebar: toggle,
  })

  // A page opened by URL that the server would refuse sends the reader to
  // their first allowed page rather than to a 403.
  if (!isPathAllowed(pathname, { isStakeholder, allowedRoutes })) {
    return <Navigate to={homePath(destinations)} replace />
  }

  const logsButton = (compact: boolean, onOpen = () => setLogs(!logsOpen)) =>
    compactable(
      compact,
      t("nav.logs"),
      <button
        type="button"
        onClick={onOpen}
        aria-pressed={logsOpen}
        aria-label={compact ? t("nav.logs") : undefined}
        className={cn(sideRow, compact ? "justify-center" : "gap-2.5 px-2.5", logsOpen && "bg-sidebar-active text-sidebar-foreground")}
      >
        <ScrollText className="h-4 w-4 shrink-0" aria-hidden />
        {compact ? null : <span>{t("nav.logs")}</span>}
      </button>,
    )

  const shareButton = (compact: boolean, onOpen = () => setShareOpen(true)) =>
    compactable(
      compact,
      t("nav.share"),
      <button
        type="button"
        onClick={onOpen}
        aria-haspopup="dialog"
        aria-label={compact ? t("nav.share") : undefined}
        className={cn(
          "flex h-8 w-full items-center justify-center gap-2 rounded-md bg-brand text-[13px] font-medium text-white transition-opacity hover:opacity-90",
          focusRing,
        )}
      >
        <Share className="h-4 w-4 shrink-0" aria-hidden />
        {compact ? null : <span>{t("nav.share")}</span>}
      </button>,
    )

  const logoutButton = (compact: boolean) =>
    compactable(
      compact,
      t("nav.logout"),
      <button
        type="button"
        onClick={handleLogout}
        aria-label={compact ? t("nav.logout") : undefined}
        className={cn(sideRow, compact ? "justify-center" : "gap-2.5 px-2.5")}
      >
        <LogOut className="h-4 w-4 shrink-0" aria-hidden />
        {compact ? null : <span>{t("nav.logout")}</span>}
      </button>,
    )

  const searchKeys = isMac() ? "⌘K" : "Ctrl K"
  const searchButton = compactable(
    collapsed,
    `${t("palette.open")} (${searchKeys})`,
    <button
      type="button"
      onClick={() => setPaletteOpen(true)}
      aria-label={collapsed ? t("palette.open") : undefined}
      aria-keyshortcuts={isMac() ? "Meta+K" : "Control+K"}
      className={cn(sideRow, "border bg-background/40", collapsed ? "justify-center" : "gap-2.5 px-2.5")}
    >
      <Search className="h-4 w-4 shrink-0" aria-hidden />
      {collapsed ? null : (
        <>
          <span className="flex-1 text-left">{t("palette.open")}</span>
          <kbd className="font-mono text-[11px] text-sidebar-muted">{searchKeys}</kbd>
        </>
      )}
    </button>,
  )

  const toggleLabel = collapsed ? t("nav.expand") : t("nav.collapse")
  const iconButton = cn(
    "flex h-10 w-10 items-center justify-center rounded-md text-sidebar-muted hover:bg-sidebar-active hover:text-sidebar-foreground",
    focusRing,
  )

  return (
    <div className="min-h-screen bg-background md:flex">
      {/* Small screens: a slim top bar, the destinations at the bottom, the rest under "More". */}
      <header className="sticky top-0 z-30 border-b bg-sidebar text-sidebar-foreground md:hidden">
        <div className="flex h-14 items-center justify-between gap-2 px-4">
          <div className="flex min-w-0 items-center gap-2.5">
            <img src="/favicon.svg" alt="" className="h-6 w-6 shrink-0" />
            <span className="truncate text-[15px] font-semibold">{current ? t(current.label) : "orch"}</span>
          </div>
          <button type="button" onClick={() => setPaletteOpen(true)} aria-label={t("palette.open")} className={iconButton}>
            <Search className="h-4 w-4" aria-hidden />
          </button>
        </div>
      </header>

      <nav
        aria-label={t("nav.main") + " (small screens)"}
        className={cn(
          "fixed inset-x-0 bottom-0 z-40 grid h-16 border-t bg-sidebar pb-[env(safe-area-inset-bottom)] text-sidebar-foreground md:hidden",
          BOTTOM_COLS[destinations.length + 1] ?? "grid-cols-5",
        )}
      >
        {destinations.map((d) => {
          const Icon = d.icon
          const active = d.id === currentId
          return (
            <NavLink
              key={d.id}
              to={d.tabs[0].to}
              aria-current={active ? "page" : undefined}
              className={cn(
                "flex flex-col items-center justify-center gap-1 text-[11px] font-medium",
                focusRing,
                active ? "text-sidebar-foreground" : "text-sidebar-muted",
              )}
            >
              <Icon className={cn("h-5 w-5", active && "text-brand")} aria-hidden />
              {t(d.label)}
            </NavLink>
          )
        })}
        <button
          type="button"
          onClick={() => setMoreOpen(true)}
          aria-haspopup="dialog"
          aria-expanded={moreOpen}
          className={cn("flex flex-col items-center justify-center gap-1 text-[11px] font-medium text-sidebar-muted", focusRing)}
        >
          <MoreHorizontal className="h-5 w-5" aria-hidden />
          {t("nav.more")}
        </button>
      </nav>

      <Dialog open={moreOpen} onOpenChange={setMoreOpen}>
        <DialogContent aria-label={t("nav.more")} className="max-w-sm gap-px self-end bg-sidebar p-3 text-sidebar-foreground md:hidden">
          {showSystem ? <p className="px-2.5 py-1 text-[11px] font-medium text-sidebar-muted">{t("nav.system")}</p> : null}
          {canSeeLogs
            ? logsButton(false, () => {
                setMoreOpen(false)
                setLogs(true)
              })
            : null}
          <div className="my-2 border-t" />
          <LanguageToggle compact={false} />
          <ThemeToggle compact={false} />
          {logoutButton(false)}
          {canShare ? (
            <div className="pt-2">
              {shareButton(false, () => {
                setMoreOpen(false)
                setShareOpen(true)
              })}
            </div>
          ) : null}
        </DialogContent>
      </Dialog>

      <aside
        className={cn(
          "fixed left-0 top-0 z-30 hidden h-screen flex-col border-r bg-sidebar text-sidebar-foreground md:flex",
          "transition-[width] duration-200 ease-out",
          collapsed ? "w-14" : "w-60",
        )}
      >
        <div className={cn("flex h-12 items-center", collapsed ? "justify-center" : "justify-between pl-4 pr-2")}>
          {collapsed ? null : (
            <div className="flex min-w-0 items-center gap-2.5">
              <img src="/favicon.svg" alt="Orch" className="h-6 w-6 shrink-0" />
              <span className="text-[14px] font-semibold tracking-[-0.01em]">orch</span>
            </div>
          )}
          <Tooltip content={`${toggleLabel} ([)`}>
            <button
              type="button"
              onClick={toggle}
              aria-label={toggleLabel}
              aria-expanded={!collapsed}
              aria-keyshortcuts="["
              className={cn(
                "flex h-7 w-7 items-center justify-center rounded-md text-sidebar-muted hover:bg-sidebar-active hover:text-sidebar-foreground",
                focusRing,
              )}
            >
              <PanelLeft className="h-4 w-4" aria-hidden />
            </button>
          </Tooltip>
        </div>

        <div className="px-2 pb-3">{searchButton}</div>

        <div className="flex-1 space-y-4 overflow-y-auto px-2">
          <nav aria-label={t("nav.main")} className="space-y-px">
            <DestinationLinks destinations={destinations} currentId={currentId} collapsed={collapsed} />
          </nav>
          {showSystem ? (
            <section aria-label={t("nav.system")} className="space-y-px">
              {collapsed ? (
                <div className="mx-2 mb-2 border-t" />
              ) : (
                <p className="px-2.5 pb-1 text-[11px] font-medium text-sidebar-muted">{t("nav.system")}</p>
              )}
              {logsButton(collapsed)}
            </section>
          ) : null}
        </div>

        <div className="space-y-px border-t p-2">
          {canShare ? <div className="pb-2">{shareButton(collapsed)}</div> : null}
          {canSeeLogs && !showSystem ? logsButton(collapsed) : null}
          <LanguageToggle compact={collapsed} />
          <ThemeToggle compact={collapsed} />
          {logoutButton(collapsed)}
        </div>
      </aside>

      <main
        className={cn(
          "min-w-0 flex-1 pb-20 transition-[margin-left] duration-200 ease-out md:pb-0",
          collapsed ? "md:ml-14" : "md:ml-60",
          logsOpen && "pb-[calc(62vh+5rem)] md:pb-[62vh]",
        )}
      >
        <PageHeader destination={current} pathname={pathname} />
        <div className="mx-auto max-w-[1600px] px-4 py-6 md:px-6 md:py-8 lg:px-10">{children}</div>
      </main>

      {canShare ? (
        <Dialog open={shareOpen} onOpenChange={setShareOpen}>
          <DialogContent className="max-w-2xl gap-5">
            <DialogHeader>
              <DialogTitle>{t("share.title")}</DialogTitle>
              <DialogDescription>{t("share.intro")}</DialogDescription>
            </DialogHeader>
            <SharePanel />
          </DialogContent>
        </Dialog>
      ) : null}

      {logsOpen ? <LogsDrawer onClose={() => setLogs(false)} collapsed={collapsed} /> : null}

      <CommandPalette
        open={paletteOpen}
        onOpenChange={setPaletteOpen}
        destinations={destinations}
        isStakeholder={isStakeholder}
        canSeeLogs={canSeeLogs}
        onOpenLogs={() => setLogs(true)}
        onShowShortcuts={() => setShortcutsOpen(true)}
      />
      <ShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} destinations={destinations} />
    </div>
  )
}
