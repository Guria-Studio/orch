/**
 * Graph page — visual DAG of the task dependency graph.
 *
 * Laid out in-house (lib/graphLayout.ts) and drawn by React: tasks are cards
 * placed left to right by dependency depth, joined by elbow connectors in an
 * SVG underneath. Colors come straight from the theme tokens (a theme switch
 * needs no re-render). Zoom (buttons + mouse wheel), pan (drag), search to
 * highlight, a phase filter that dims the rest, and a click opens the task.
 * Operator-only.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { AlertTriangle, Maximize2, Minimize2, Minus, Plus, Scan, Search } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyTasks } from "@/components/EmptyTasks"
import { Explain } from "@/components/Explain"
import { useFullscreen } from "@/hooks/useFullscreen"
import { useGraph } from "@/hooks/useGraph"
import { t } from "@/i18n"
import { layoutGraph, NODE_WIDTH } from "@/lib/graphLayout"
import { taskPath } from "@/lib/nav"
import { criticalPathMatters, STATUS, statusKey, type StatusKey } from "@/lib/status"
import { cn } from "@/lib/utils"

// ---- Drawing ---------------------------------------------------------------

const STATUS_VAR: Record<StatusKey, string> = {
  backlog: "var(--status-queued)",
  todo: "var(--status-queued)",
  in_progress: "var(--status-running)",
  blocked: "var(--status-blocked)",
  failed: "var(--status-failed)",
  done: "var(--status-done)",
}

// Cards, left to right. lib/graphLayout lays the DAG top to bottom; its layers
// become columns and its order within a layer becomes rows.
const CARD_W = 220
const CARD_H = 56
const COL_GAP = 70
const ROW_GAP = 24
const LAYOUT_STEP = NODE_WIDTH + 32 // graphLayout's NODE_WIDTH + GAP_X

const ringStyle = (status: StatusKey) => ({ borderColor: STATUS_VAR[status] })

// ---- Zoom / pan hook -------------------------------------------------------

const ZOOM_MIN = 0.25
const ZOOM_MAX = 3
const ZOOM_STEP = 0.2

const clampZoom = (z: number) => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, z))

/**
 * `fit` is the zoom that shows the whole width of the drawing. Until the reader
 * zooms, the view follows it (a resized viewport refits); reset returns to it.
 * `dragged` tells a click that ends a pan from a click on a card.
 */
function useZoomPan(fit: number) {
  const [userZoom, setUserZoom] = useState<number | null>(null)
  const zoom = userZoom ?? clampZoom(fit)
  const [pan, setPan] = useState({ x: 0, y: 0 })
  const dragging = useRef(false)
  const dragged = useRef(false)
  const lastPos = useRef({ x: 0, y: 0 })

  const step = useCallback(
    (delta: number) => setUserZoom((z) => clampZoom(+((z ?? clampZoom(fit)) + delta).toFixed(2))),
    [fit],
  )
  const zoomIn = () => step(ZOOM_STEP)
  const zoomOut = () => step(-ZOOM_STEP)
  const reset = useCallback(() => {
    setUserZoom(null)
    setPan({ x: 0, y: 0 })
  }, [])

  const onWheel = useCallback(
    (e: WheelEvent) => {
      e.preventDefault()
      step(e.deltaY > 0 ? -ZOOM_STEP : ZOOM_STEP)
    },
    [step],
  )

  const onMouseDown = useCallback((e: MouseEvent) => {
    if (e.button !== 0) return
    dragging.current = true
    dragged.current = false
    lastPos.current = { x: e.clientX, y: e.clientY }
  }, [])

  const onMouseMove = useCallback((e: MouseEvent) => {
    if (!dragging.current) return
    const dx = e.clientX - lastPos.current.x
    const dy = e.clientY - lastPos.current.y
    if (dx !== 0 || dy !== 0) dragged.current = true
    lastPos.current = { x: e.clientX, y: e.clientY }
    setPan((p) => ({ x: p.x + dx, y: p.y + dy }))
  }, [])

  const onMouseUp = useCallback(() => {
    dragging.current = false
  }, [])

  return { zoom, pan, dragged, zoomIn, zoomOut, reset, onWheel, onMouseDown, onMouseMove, onMouseUp }
}

// ---- Component -------------------------------------------------------------

const LEGEND: StatusKey[] = ["in_progress", "blocked", "done", "todo"]

const controlButton =
  "flex h-8 w-8 items-center justify-center rounded-md border bg-card text-muted-foreground shadow-sm transition-colors hover:bg-secondary hover:text-foreground"

export function GraphPage() {
  const { data, isLoading, isError, error } = useGraph()
  const navigate = useNavigate()
  const viewportRef = useRef<HTMLDivElement>(null)
  const graphRef = useRef<HTMLDivElement>(null)
  const [isFullscreen, toggleFullscreen] = useFullscreen(graphRef)
  const [query, setQuery] = useState("")
  const [phase, setPhase] = useState("")

  // Left-to-right placement from the top-to-bottom layout.
  const drawing = useMemo(() => {
    if (!data) return null
    const layout = layoutGraph(data.nodes, data.edges)
    const layerYs = [...new Set(layout.nodes.map((n) => n.y))].sort((a, b) => a - b)
    const pos = new Map(
      layout.nodes.map((n) => [
        n.id,
        { x: layerYs.indexOf(n.y) * (CARD_W + COL_GAP), y: Math.round((n.x / LAYOUT_STEP) * (CARD_H + ROW_GAP)) },
      ]),
    )
    const edges = layout.edges.map((e) => {
      const s = pos.get(e.source)!
      const d = pos.get(e.target)!
      const x1 = s.x + CARD_W
      const y1 = s.y + CARD_H / 2
      const x2 = d.x
      const y2 = d.y + CARD_H / 2
      const mx = Math.round((x1 + x2) / 2)
      return { source: e.source, target: e.target, d: `M${x1},${y1} H${mx} V${y2} H${x2}` }
    })
    const width = Math.max(layerYs.length * (CARD_W + COL_GAP) - COL_GAP, CARD_W)
    const height = Math.max(CARD_H, ...[...pos.values()].map((p) => p.y + CARD_H))
    return { pos, edges, width, height }
  }, [data])

  const [viewportWidth, setViewportWidth] = useState(0)
  useEffect(() => {
    const el = viewportRef.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => setViewportWidth(Math.round(entry.contentRect.width)))
    observer.observe(el)
    return () => observer.disconnect()
  }, [drawing])
  const fit = drawing && viewportWidth ? Math.min(1, (viewportWidth - 80) / drawing.width) : 1

  const { zoom, pan, dragged, zoomIn, zoomOut, reset, onWheel, onMouseDown, onMouseMove, onMouseUp } = useZoomPan(fit)

  // Attach wheel + drag to the viewport
  useEffect(() => {
    const el = viewportRef.current
    if (!el) return
    el.addEventListener("wheel", onWheel, { passive: false })
    el.addEventListener("mousedown", onMouseDown)
    window.addEventListener("mousemove", onMouseMove)
    window.addEventListener("mouseup", onMouseUp)
    return () => {
      el.removeEventListener("wheel", onWheel)
      el.removeEventListener("mousedown", onMouseDown)
      window.removeEventListener("mousemove", onMouseMove)
      window.removeEventListener("mouseup", onMouseUp)
    }
  }, [onWheel, onMouseDown, onMouseMove, onMouseUp, data])

  const nodes = data?.nodes ?? []
  const nodeCount = nodes.length
  const edgeCount = data?.edges.length ?? 0
  const showCritical = data ? criticalPathMatters(data.nodes) : false
  const nodeById = useMemo(() => new Map((data?.nodes ?? []).map((n) => [n.id, n])), [data])
  const phases = useMemo(() => [...new Set((data?.nodes ?? []).map((n) => n.phase))].sort((a, b) => a - b), [data])

  // The critical path is a trace along its edges, drawn only when the project
  // has branches; in a single chain it would mark every edge.
  const onCritical = (source: string, target: string) =>
    showCritical && Boolean(nodeById.get(source)?.on_critical_path && nodeById.get(target)?.on_critical_path)

  // Search highlights, the phase filter dims the rest; the drawing never moves.
  const needle = query.trim().toLowerCase()
  const filtering = needle !== "" || phase !== ""
  const lit = (id: string) => {
    const n = nodeById.get(id)
    if (!n || (phase !== "" && String(n.phase) !== phase)) return false
    return needle === "" || n.id.toLowerCase().includes(needle) || n.label.toLowerCase().includes(needle)
  }

  return (
    <div className="flex h-full flex-col gap-4 text-[13px]">
      <h1 className="sr-only">{t("graph.title")}</h1>
      {isLoading ? (
        <Skeleton className="h-96 w-full rounded-lg" />
      ) : isError ? (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("graph.load_failed")}</AlertTitle>
          <AlertDescription>{error?.message ?? t("common.unknown_error")}</AlertDescription>
        </Alert>
      ) : nodeCount === 0 || !drawing ? (
        <EmptyTasks />
      ) : (
        <div ref={graphRef} className="flex min-h-[480px] flex-1 flex-col overflow-hidden rounded-lg border bg-background">
          <div className="flex flex-wrap items-center gap-2 border-b px-3.5 py-2.5">
            <label className="flex h-8 min-w-[220px] items-center gap-2 rounded-md border bg-card px-2.5 text-muted-foreground max-md:h-10">
              <Search className="h-3.5 w-3.5 shrink-0" aria-hidden />
              <input
                type="search"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t("graph.highlight")}
                aria-label={t("graph.highlight")}
                className="min-w-0 flex-1 bg-transparent text-foreground outline-none placeholder:text-muted-foreground"
              />
            </label>
            <select
              value={phase}
              onChange={(e) => setPhase(e.target.value)}
              aria-label={t("filters.phase_label")}
              className="h-8 rounded-md border bg-card px-2 text-[13px] text-foreground max-md:h-10"
            >
              <option value="">{t("filters.all_phases")}</option>
              {phases.map((p) => (
                <option key={p} value={String(p)}>
                  {t("filters.phase", { phase: p })}
                </option>
              ))}
            </select>
            <div className="ml-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
              {LEGEND.map((key) => (
                <span key={key} className="inline-flex items-center gap-1.5">
                  <span className="h-2.5 w-2.5 rounded-full border-2" style={ringStyle(key)} aria-hidden />
                  {t(STATUS[key].label)}
                </span>
              ))}
              {showCritical ? (
                <span className="inline-flex items-center gap-1.5">
                  <span className="inline-block h-0.5 w-4 rounded-full bg-brand" aria-hidden />
                  <Explain term="critical_path" />
                </span>
              ) : null}
            </div>
            <div className="ml-auto flex items-center gap-2">
              <span className="text-xs tabular-nums text-muted-foreground">{t("graph.counts", { tasks: nodeCount, edges: edgeCount })}</span>
              <Button type="button" variant="outline" size="sm" onClick={toggleFullscreen}>
                {isFullscreen ? <Minimize2 className="h-3.5 w-3.5" aria-hidden /> : <Maximize2 className="h-3.5 w-3.5" aria-hidden />}
                {isFullscreen ? t("common.exit_fullscreen") : t("common.fullscreen")}
              </Button>
            </div>
          </div>

          <div
            ref={viewportRef}
            className="relative min-h-0 w-full flex-1 cursor-grab select-none overflow-hidden active:cursor-grabbing"
            style={{ backgroundImage: "radial-gradient(var(--border) 1px, transparent 1px)", backgroundSize: "20px 20px" }}
          >
            <div
              className="relative"
              style={{
                width: drawing.width,
                height: drawing.height,
                transform: `translate(${pan.x + 40}px, ${pan.y + 40}px) scale(${zoom})`,
                transformOrigin: "top left",
                transition: "transform 0.05s ease-out",
              }}
            >
              <svg width={drawing.width} height={drawing.height} className="absolute inset-0 overflow-visible" aria-hidden>
                {drawing.edges.map((e) => {
                  const critical = onCritical(e.source, e.target)
                  const dim = filtering && !(lit(e.source) && lit(e.target))
                  return (
                    <path
                      key={`${e.source}->${e.target}`}
                      d={e.d}
                      style={{
                        fill: "none",
                        stroke: critical ? "var(--brand)" : "var(--input)",
                        strokeWidth: critical ? 2 : 1,
                        opacity: dim ? 0.25 : 1,
                      }}
                    />
                  )
                })}
              </svg>
              <ul aria-label={t("graph.title")}>
                {nodes.map((n) => {
                  const p = drawing.pos.get(n.id)!
                  const status = statusKey(n.status)
                  const on = lit(n.id)
                  return (
                    <li key={n.id} className="absolute" style={{ left: p.x, top: p.y, width: CARD_W, height: CARD_H }}>
                      <button
                        type="button"
                        title={`${n.id} · ${n.label} · ${t(STATUS[status].label)}`}
                        onClick={() => {
                          if (!dragged.current) navigate(taskPath(n.id))
                        }}
                        className={cn(
                          "flex h-full w-full flex-col justify-center gap-1 rounded-lg border bg-card px-3 text-left shadow-sm transition-opacity hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                          showCritical && n.on_critical_path && "border-brand",
                          filtering && !on && "opacity-30",
                          on && needle !== "" && "ring-2 ring-brand",
                        )}
                      >
                        <span className="flex items-center gap-1.5">
                          <span className="h-2.5 w-2.5 shrink-0 rounded-full border-2" style={ringStyle(status)} aria-hidden />
                          <span className="font-mono text-[11px] text-muted-foreground">{n.id}</span>
                          <span className="sr-only">{t(STATUS[status].label)}</span>
                        </span>
                        <span className="truncate text-xs font-medium">{n.label}</span>
                      </button>
                    </li>
                  )
                })}
              </ul>
            </div>

            <div className="absolute bottom-4 left-4 flex flex-col gap-1" onMouseDown={(e) => e.stopPropagation()}>
              <button type="button" onClick={zoomIn} aria-label={t("graph.zoom_in")} className={controlButton}>
                <Plus className="h-4 w-4" aria-hidden />
              </button>
              <button type="button" onClick={zoomOut} aria-label={t("graph.zoom_out")} className={controlButton}>
                <Minus className="h-4 w-4" aria-hidden />
              </button>
              <button
                type="button"
                onClick={reset}
                aria-label={t("graph.reset_zoom")}
                title={`${t("graph.reset_zoom")} · ${Math.round(zoom * 100)}%`}
                className={controlButton}
              >
                <Scan className="h-4 w-4" aria-hidden />
              </button>
            </div>
            <span className="pointer-events-none absolute bottom-4 right-4 text-xs text-muted-foreground">{t("graph.hint")}</span>
          </div>
        </div>
      )}
    </div>
  )
}
