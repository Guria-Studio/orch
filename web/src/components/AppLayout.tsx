import { useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Navigate, NavLink, useLocation, useNavigate } from "react-router-dom"
import { Languages, LogOut, Monitor, Moon, MoreHorizontal, PanelLeft, Search, Share, Sun } from "lucide-react"
import { CommandPalette, ShortcutsDialog } from "@/components/CommandPalette"
import { SharePanel } from "@/components/SharePanel"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Tooltip } from "@/components/ui/tooltip"
import { LANGS, LANGUAGE_NAMES, setLanguage, t, useLanguage } from "@/i18n"
import { useAuth } from "@/hooks/useAuth"
import type { Now } from "@/hooks/useNow"
import { useTunnelCapabilities } from "@/hooks/useTunnelCapabilities"
import { isPortfolioNavVisible, usePortfolio } from "@/hooks/usePortfolio"
import { useSidebarCollapsed } from "@/hooks/useSidebarCollapsed"
import { useWhoami } from "@/hooks/useWhoami"
import { releasesSeen, useReleases } from "@/hooks/useReleases"
import { TAB_ICONS, findTab, homePath, isPathAllowed, visibleDestinations, type NavDestination } from "@/lib/nav"
import { apiClient } from "@/lib/api"
import { isMac, useKeyboardShortcuts } from "@/lib/shortcuts"
import { nextTheme, useTheme } from "@/lib/theme"
import { cn } from "@/lib/utils"

interface AppLayoutProps {
  children: ReactNode
}

const THEME_ICON = { dark: Moon, light: Sun, system: Monitor } as const
// The phone's bottom bar: the visible destinations plus "More".
const BOTTOM_COLS: Record<number, string> = { 2: "grid-cols-2", 3: "grid-cols-3", 4: "grid-cols-4", 5: "grid-cols-5" }
const SHARE_PATH = "/delivery/share"
const LOGS_PATH = "/system/logs"

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
function DestinationLinks({
  destinations,
  currentId,
  collapsed,
  badges = {},
}: {
  destinations: NavDestination[]
  currentId?: string
  collapsed: boolean
  /** A short note at the end of a destination's row, by destination id ("3 in flight" on Now). */
  badges?: Record<string, ReactNode>
}) {
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
                {collapsed || !badges[d.id] ? null : <span className="ml-auto text-[11px] font-normal text-status-running">{badges[d.id]}</span>}
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
function PageHeader({
  destination,
  pathname,
  project,
  runLive,
}: {
  destination?: NavDestination
  pathname: string
  project: string
  runLive: boolean
}) {
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
          <span className="text-muted-foreground">{project}</span>
          <span className="text-muted-foreground" aria-hidden>
            ›
          </span>
          <span className="font-medium">{t(destination.label)}</span>
          {/* The page's own name only when no view tab already says it. */}
          {currentTab && currentTab.label !== destination.label && (tabs.length < 2 || currentTab.offTabs) ? (
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
        {runLive ? (
          <span className="ml-auto inline-flex shrink-0 items-center gap-1.5 rounded-full bg-status-done/14 px-2 py-0.5 text-xs text-status-done">
            <span className="h-1.5 w-1.5 rounded-full bg-status-done" aria-hidden />
            {t("shell.run_live")}
          </span>
        ) : null}
      </div>
    </div>
  )
}

export function AppLayout({ children }: AppLayoutProps) {
  const { clearToken } = useAuth()
  const navigate = useNavigate()
  const [collapsed, , toggle] = useSidebarCollapsed()
  const { data: whoami } = useWhoami()
  const { pathname } = useLocation()
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

  // The workspace block and the live signals: the project, its repository,
  // what is running. /api/now is the operator's (the Now page reads the same
  // cache entry); the repository comes with the Share panel's capabilities,
  // for the local operator only.
  const { data: now } = useQuery({
    queryKey: ["tasks", "now"],
    queryFn: async () => (await apiClient.get<Now>("/api/now")).data,
    enabled: !isStakeholder,
    refetchInterval: 30_000,
  })
  const { data: caps } = useTunnelCapabilities()
  const project = now?.project || "orch"
  const repoSlug = caps?.repo?.slug
  const inFlight = now?.working.length ?? 0
  const runLive = now?.run?.status === "live"
  const isLocal = ["127.0.0.1", "localhost", "[::1]", "::1"].includes(window.location.hostname)

  const destinations = visibleDestinations({ isStakeholder, allowedRoutes, portfolioAvailable })
  // The main navigation is the four places; System's pages are listed apart.
  const places = destinations.filter((d) => d.id !== "system")
  const systemTabs = destinations.find((d) => d.id === "system")?.tabs ?? []
  const currentId = findTab(pathname)?.destination.id
  const current = destinations.find((d) => d.id === currentId)
  const canSeeLogs = destinations.some((d) => d.tabs.some((tab) => tab.to === LOGS_PATH))
  const canShare = destinations.some((d) => d.tabs.some((tab) => tab.to === SHARE_PATH))
  const showSystem = systemTabs.length > 0

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
          <span className="flex-1 text-left">{t("shell.search")}</span>
          <kbd className="font-mono text-[11px] text-sidebar-muted">{searchKeys}</kbd>
        </>
      )}
    </button>,
  )

  // System's pages, as sidebar rows.
  const systemLinks = (compact: boolean, onNavigate?: () => void) =>
    systemTabs.map((tab) => {
      const Icon = TAB_ICONS[tab.to]
      const active = pathname === tab.to
      return (
        <div key={tab.to}>
          {compactable(
            compact,
            t(tab.label),
            <NavLink
              to={tab.to}
              onClick={onNavigate}
              aria-current={active ? "page" : undefined}
              aria-label={compact ? t(tab.label) : undefined}
              className={cn(sideRow, compact ? "justify-center" : "gap-2.5 px-2.5", active && "bg-sidebar-active font-medium text-sidebar-foreground")}
            >
              {Icon ? <Icon className={cn("h-4 w-4 shrink-0", active && "text-brand")} aria-hidden /> : null}
              {compact ? null : <span>{t(tab.label)}</span>}
            </NavLink>,
          )}
        </div>
      )
    })

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
          BOTTOM_COLS[places.length + 1] ?? "grid-cols-5",
        )}
      >
        {places.map((d) => {
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
          {systemLinks(false, () => setMoreOpen(false))}
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
              <img src="/favicon.svg" alt="orch" className="h-6 w-6 shrink-0" />
              <span className="flex min-w-0 flex-col leading-tight">
                <span className="truncate text-[13px] font-semibold">{project}</span>
                {repoSlug ? <span className="truncate text-[11px] text-sidebar-muted">{repoSlug}</span> : null}
              </span>
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
            <DestinationLinks
              destinations={places}
              currentId={currentId}
              collapsed={collapsed}
              badges={inFlight > 0 ? { now: t("shell.in_flight", { count: inFlight }) } : undefined}
            />
          </nav>
          {showSystem ? (
            <section aria-label={t("nav.system")} className="space-y-px">
              {collapsed ? (
                <div className="mx-2 mb-2 border-t" />
              ) : (
                <p className="px-2.5 pb-1 text-[11px] font-medium text-sidebar-muted">{t("nav.system")}</p>
              )}
              {systemLinks(collapsed)}
            </section>
          ) : null}
        </div>

        <div className="space-y-2 p-2">
          {canShare ? shareButton(collapsed) : null}
          {/* Who this session is and where it is served from, with the session's controls beside it. */}
          <div className={cn("flex items-center", collapsed ? "flex-col gap-px" : "gap-1 pl-2.5")}>
            {collapsed ? null : (
              <span className="flex min-w-0 flex-1 items-center gap-2 text-xs text-sidebar-muted">
                <span className={cn("h-2 w-2 shrink-0 rounded-full", isLocal ? "bg-status-done" : "bg-status-running")} aria-hidden />
                <span className="truncate">
                  {t(isStakeholder ? "shell.client" : "shell.operator")} · {t(isLocal ? "shell.local" : "shell.remote")}
                </span>
              </span>
            )}
            <div className={cn("flex", collapsed ? "w-full flex-col gap-px" : "shrink-0 [&>*]:w-8")}>
              <LanguageToggle compact />
              <ThemeToggle compact />
              {logoutButton(true)}
            </div>
          </div>
        </div>
      </aside>

      <main
        className={cn(
          "min-w-0 flex-1 pb-20 transition-[margin-left] duration-200 ease-out md:pb-0",
          collapsed ? "md:ml-14" : "md:ml-60",
        )}
      >
        <PageHeader destination={current} pathname={pathname} project={project} runLive={runLive} />
        <div className="w-full px-4 py-6 md:px-6">{children}</div>
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

      <CommandPalette
        open={paletteOpen}
        onOpenChange={setPaletteOpen}
        destinations={destinations}
        isStakeholder={isStakeholder}
        canSeeLogs={canSeeLogs}
        onOpenLogs={() => navigate(LOGS_PATH)}
        onShowShortcuts={() => setShortcutsOpen(true)}
      />
      <ShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} destinations={destinations} />
    </div>
  )
}
