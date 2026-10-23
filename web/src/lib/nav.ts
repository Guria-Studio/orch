import {
  Activity,
  BarChart3,
  BookOpen,
  BriefcaseBusiness,
  CalendarClock,
  Gauge,
  Eye,
  FileText,
  FlaskConical,
  GitFork,
  KanbanSquare,
  LayoutGrid,
  ListIcon,
  Milestone,
  Radio,
  ScrollText,
  Send,
  Settings,
  Sparkles,
  Wallet,
  Workflow,
  Wrench,
} from "lucide-react"
import type { MessageKey } from "@/i18n"

export interface NavTab {
  to: string
  label: MessageKey
  // The server route the page reads its data from, by the name the Go
  // dashboard registers it under. A stakeholder sees a page only when this
  // route is on the allow-list /api/whoami returns, so the navigation and the
  // gate share one rule instead of two copies that drift apart.
  route: string
  // Used only when a server sends no allow-list (an older binary).
  operatorOnly?: boolean
  // G8.5: hidden whenever the dashboard wasn't started with --portfolio.
  portfolioGated?: boolean
  // A page reached from elsewhere (Share: the sidebar's button), so it is not
  // one of its destination's view tabs. Still gated and addressable.
  offTabs?: boolean
}

export interface NavDestination {
  id: "now" | "work" | "cost" | "delivery" | "system"
  label: MessageKey
  icon: typeof Activity
  tabs: NavTab[]
}

/**
 * Four places, each answering one question: what is happening now, where the
 * work stands, what it costs, and what has been delivered — plus System, the
 * machine and project setup, listed apart. Every page is a tab of exactly one
 * of them.
 */
export const DESTINATIONS: NavDestination[] = [
  {
    id: "now",
    label: "nav.now",
    icon: Activity,
    tabs: [
      { to: "/", label: "nav.now", route: "api_now", operatorOnly: true },
      { to: "/now/portfolio", label: "nav.portfolio", route: "api_portfolio", operatorOnly: true, portfolioGated: true },
      { to: "/now/releases", label: "nav.releases", route: "api_releases", operatorOnly: true },
    ],
  },
  {
    id: "work",
    label: "nav.work",
    icon: BriefcaseBusiness,
    tabs: [
      { to: "/work/list", label: "nav.list", route: "api_tasks" },
      { to: "/work/kanban", label: "nav.kanban", route: "api_tasks" },
      { to: "/work/graph", label: "nav.graph", route: "api_graph", operatorOnly: true },
      { to: "/work/phases", label: "nav.phases", route: "api_milestones" },
      { to: "/work/pace", label: "nav.pace", route: "api_sprint" },
    ],
  },
  {
    id: "cost",
    label: "nav.usage",
    icon: Wallet,
    tabs: [
      { to: "/cost/usage", label: "nav.usage", route: "api_metrics", operatorOnly: true },
      { to: "/cost/budget", label: "nav.budget", route: "api_budget_summary" },
    ],
  },
  {
    id: "delivery",
    label: "nav.delivery",
    icon: Send,
    tabs: [
      // First: it is where a client's link lands, and what the operator checks before sending one.
      { to: "/delivery/client", label: "nav.client", route: "stakeholder_snapshot_json" },
      { to: "/delivery/summary", label: "nav.summary", route: "stakeholder_summary_json" },
      { to: "/delivery/ci", label: "nav.ci", route: "api_ci", operatorOnly: true },
      { to: "/delivery/tests", label: "nav.tests", route: "api_tests", operatorOnly: true },
      { to: "/delivery/share", label: "nav.share", route: "api_tunnel_status", operatorOnly: true, offTabs: true },
    ],
  },
  // System is not a fifth place in the main navigation: the sidebar lists its
  // pages under their own heading, and the phone keeps them behind More.
  {
    id: "system",
    label: "nav.system",
    icon: Wrench,
    tabs: [
      { to: "/system/providers", label: "nav.providers", route: "api_quota", operatorOnly: true },
      { to: "/system/tools", label: "nav.tools", route: "api_tools", operatorOnly: true },
      { to: "/system/skills", label: "nav.skills", route: "api_skills", operatorOnly: true },
      // The event log; a stakeholder whose allow-list grants it sees it too.
      { to: "/system/logs", label: "nav.logs", route: "api_events", operatorOnly: true },
      { to: "/system/settings", label: "nav.settings", route: "api_config_effective", operatorOnly: true },
    ],
  },
]

/** Icons for tabs, keyed by path; the destination icon stands for the group. */
export const TAB_ICONS: Record<string, typeof Activity> = {
  "/": Activity,
  "/now/portfolio": LayoutGrid,
  "/now/releases": Sparkles,
  "/work/list": ListIcon,
  "/work/kanban": KanbanSquare,
  "/work/graph": GitFork,
  "/work/phases": Milestone,
  "/work/pace": CalendarClock,
  "/cost/budget": Wallet,
  "/cost/usage": BarChart3,
  "/system/providers": Gauge,
  "/delivery/client": Eye,
  "/delivery/summary": FileText,
  "/delivery/ci": Workflow,
  "/delivery/tests": FlaskConical,
  "/delivery/share": Radio,
  "/system/tools": Wrench,
  "/system/skills": BookOpen,
  "/system/settings": Settings,
  "/system/logs": ScrollText,
}

/**
 * Every address the dashboard answered before it had four destinations, and
 * where it lives now. Bookmarks and shared links keep working.
 */
export const LEGACY_REDIRECTS: Record<string, string> = {
  "/list": "/work/list",
  "/kanban": "/work/kanban",
  "/graph": "/work/graph",
  "/milestones": "/work/phases",
  "/pace": "/work/pace",
  "/sprint": "/work/pace",
  "/budget": "/cost/budget",
  "/metrics": "/cost/usage",
  "/cost/metrics": "/cost/usage",
  "/ci": "/delivery/ci",
  "/tests": "/delivery/tests",
  "/tunnel": "/delivery/share",
  "/logs": "/system/logs",
  "/portfolio": "/now/portfolio",
  "/summary": "/delivery/summary",
}

interface Access {
  isStakeholder: boolean
  // /api/whoami's `routes`: present only for a stakeholder session.
  allowedRoutes?: string[]
}

function maySee(tab: NavTab, { isStakeholder, allowedRoutes, portfolioAvailable }: Access & { portfolioAvailable: boolean }) {
  if (isStakeholder) return allowedRoutes ? allowedRoutes.includes(tab.route) : !tab.operatorOnly
  return !tab.portfolioGated || portfolioAvailable
}

/** The destinations this session may see, each holding only its visible tabs. */
export function visibleDestinations(access: Access & { portfolioAvailable: boolean }): NavDestination[] {
  return DESTINATIONS.map((d) => ({ ...d, tabs: d.tabs.filter((tab) => maySee(tab, access)) })).filter(
    (d) => d.tabs.length > 0,
  )
}

/** The address of a task's page (#374). */
export function taskPath(id: string): string {
  return `/work/tasks/${encodeURIComponent(id)}`
}

/** The tab a path belongs to: an exact match, or the tab whose path it extends. */
export function findTab(pathname: string): { destination: NavDestination; tab: NavTab } | undefined {
  // A task's page belongs to the task list: same destination, same data route.
  if (pathname.startsWith("/work/tasks/")) pathname = "/work/list"
  for (const destination of DESTINATIONS) {
    for (const tab of destination.tabs) {
      if (tab.to === "/" ? pathname === "/" : pathname === tab.to || pathname.startsWith(`${tab.to}/`)) {
        return { destination, tab }
      }
    }
  }
  return undefined
}

/** Where a session lands: its first visible page. */
export function homePath(destinations: NavDestination[]): string {
  return destinations[0]?.tabs[0]?.to ?? "/login"
}

// isPathAllowed answers whether a page opened by URL should render, or send
// a stakeholder to their home page instead of a 403.
export function isPathAllowed(pathname: string, { isStakeholder, allowedRoutes }: Access): boolean {
  if (!isStakeholder) return true
  const found = findTab(pathname)
  if (!found) return true
  return allowedRoutes ? allowedRoutes.includes(found.tab.route) : !found.tab.operatorOnly
}
