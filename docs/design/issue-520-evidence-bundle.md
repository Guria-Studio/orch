# #520 — Paquete de evidencia por tarea

Estado: **propuesta para revisión** (no implementado) · Paraguas: #518 · Relacionados: #521, #523

## 1. Problema

`internal/verify` ya corre los checks del repo, los E2E y una review de otro
proveedor, pero lo que prueba queda repartido entre eventos de `orch.db`, un
directorio de la state dir y la página **Delivery → Tests** del dashboard del
operador. Ni el PR ni la página del cliente dicen *cómo* se probó una tarea.
Además, el arranque de la app solo comprueba que responda por debajo de 500,
así que un panel que devuelve 200 con "no está construida" pasa.

Objetivo: por tarea, un **paquete de evidencia** con ruta estable, que se
resume en un comentario del PR y en la página del cliente, sin filtrar secretos.

## 2. Qué existe hoy (código)

| Pieza | Dónde | Qué deja |
|---|---|---|
| Gate local | `internal/verify/verify.go:78-105` | checks → E2E → review; devuelve `contract.VerifyResult` |
| Arranque de la app | `verify.go:128-162`, `waitReady` `:210-237` | acepta cualquier status `< 500` (un 404 cuenta como listo) |
| Reporte E2E | `internal/verify/e2ereport.go:136-170` | `report.json` propio + adjuntos copiados (screenshot, video, trace) |
| Límite por adjunto | `e2ereport.go:21` | 100 MiB por archivo; sin tope por tarea |
| Ruta de artefactos | `internal/engine/verifygate.go:105-109` | `<state>/e2e/<run>/<task>/attempt-<n>/` (**cambia por run**) |
| Eventos | `verifygate.go:80-124` | `verify_passed` (comandos, reviewer, findings), `verify_failed`, `e2e_report` (conteos + ruta) |
| Cuerpo del PR | `internal/engine/reaper.go:337-341`, `:724-754` | texto de la tarea + archivos fuera de scope + avisos del guard + "no revisado" (`verifygate.go:179-186`) |
| Comentario con marcador | `internal/vcs/comment.go:33` | `UpsertComment` (gh), hoy solo lo usa `orch ci review` (`internal/cli/ci_review.go:23`) |
| Dashboard operador | `internal/dashboard/testsroutes.go:29-35,162-175` | lista de runs E2E y archivos, solo operador |
| Snapshot del cliente | `internal/publish/snapshot/snapshot.go:39` (`Schema = 1`), `:159-165` | `Deliverable` = título + estado; `Quality` (`quality.go:8-17`) solo cuenta CI |
| Límites de cloud | `internal/publish/cloud.go:33-34` | 5 MiB por archivo, 10 MiB por subida |
| Redacción | `internal/findings/findings.go:57` (`Redact`), `internal/guard/guard.go:77` (`Scan`) | ya existen para findings y para el diff |
| Receta Playwright | `docs/CONFIG.md:880-910`, skill `orch-e2e` | `video: 'retain-on-failure'`: **una tarea que pasa no deja video** |

Huecos: no hay ruta por tarea (solo por run), no hay retención, la evidencia
de una tarea que pasa casi no tiene medios, y nada sale del equipo del operador.

## 3. Diseño

### 3.1 Manifiesto y ruta estable

Los archivos E2E se quedan donde están (no se mueven: el dashboard ya los lee).
Se agrega un **manifiesto** por tarea:

```
.orchestrator/state/<project>/evidence/<task>/
  evidence.json                  # el del último intento empujado (pushed)
  history/<run>-attempt-<n>.json # uno por intento verificado, pase o falle
```

`evidence.json` (versión 1) lo escribe el engine al registrar el veredicto
(`recordVerify`/`recordE2E`), no `internal/verify`, que sigue sin conocer runs:

```json
{
  "version": 1, "task_id": "F2.T3", "title": "…", "run_id": "…", "attempt": 2,
  "head_sha": "…", "base_sha": "…", "verified_at": "2026-11-20T12:00:00Z",
  "verdict": "passed | failed | review_unavailable",
  "checks":  [{"command": "go test ./...", "duration_s": 41.2, "kind": "test"}],
  "probes":  [{"path": "/", "status": 200, "ok": true, "detail": ""}],
  "review":  {"reviewer": "gemini", "blocking": 0, "findings": [{"severity": "minor", "file": "…", "line": 3, "title": "…"}]},
  "diff":    {"files": 7, "insertions": 120, "deletions": 14, "top": ["…"]},
  "e2e":     {"passed": 5, "failed": 0, "flaky": 0, "skipped": 0,
              "dir": "e2e/<run>/<task>/attempt-2",
              "media": [{"test": "login › entra", "kind": "video", "path": "t001/video.webm", "bytes": 812345}]}
}
```

Rutas siempre relativas a la state dir. `kind` del check se deduce del
comando con una heurística como la que `ci.Gates` aplica a los workflows
(`internal/ci/workflows.go:165`); los detectados por `ci.Detect` ya traen su
tipo (`verify.go:261`). Lectura: `orch evidence <task> [--json]`
(comando nuevo, fila en `docs/CLI.md`) y la página Tests del dashboard.

### 3.2 Comentario del PR

Tras abrir el PR (`openPR`, `reaper.go:378`) y en cada empuje posterior
(retry de CI, conflict retry), `UpsertComment` con marcador
`<!-- orch:evidence -->`: un solo comentario editado en sitio. En inglés,
como el resto de lo que orch escribe en PRs:

```markdown
<!-- orch:evidence -->
### Evidence — F2.T3 (attempt 2)
| Check | Result |
|---|---|
| tests · lint · build (3 commands, 58 s) | ✅ |
| app probes (2) | ✅ `/` 200, `/dashboard` contains "Orders" |
| E2E | ✅ 5 passed · 0 failed |
| review (gemini) | ✅ no blocking findings · 2 minor |

Diff: 7 files, +120 −14. Video: <link o "kept locally: `orch evidence F2.T3`">
```

Los links a medios solo aparecen si hay un destino alcanzable (§3.4); si no,
el comentario lo dice. GitLab: misma función con `glab api` (fase aparte).

### 3.3 Chequeos de contenido (no solo HTTP 200)

Nueva clave `verify.app.probes`, corrida después de `waitReady` y antes de
`verify.e2e`; un probe que falla devuelve la tarea al agente como un test rojo:

```yaml
verify:
  app:
    probes:
      - path: /
        status: 200                 # por defecto: < 400
        contains: ["Orders"]
        not_contains: ["not built", "Cannot GET"]
```

Sin `probes` no cambia nada (compatibilidad). El resultado va al manifiesto.

### 3.4 Video/trace de Playwright y página del cliente

- **Captura**: orch exporta `ORCH_E2E_VIDEO=on|retain-on-failure` según
  `evidence.video` y la receta de `docs/CONFIG.md` y del skill `orch-e2e` pasa a
  `video: process.env.ORCH_E2E_VIDEO ?? 'retain-on-failure'`. orch no reescribe
  la config de Playwright del proyecto.
- **Traces nunca salen del equipo**: guardan red, cookies y storage. Solo el
  dashboard del operador.
- **Snapshot schema 2** (aditivo): `Deliverable.evidence` opcional con
  `checked` (lista de `ci.Gates` que pasaron), `reviewed` (bool), `e2e`
  (`passed`/`total`), `steps` (títulos de `test.step`, para "esto se probó así"),
  `video`/`poster` (rutas dentro del sitio). Sin ids, modelos, comandos ni
  texto de findings, como hoy (`quality.go:5-7`).
- **Export**: `orch publish --to dir|git` copia el video elegido (el del primer
  test que pasó, o el marcado `@evidence`) a `evidence/<hash>/`. `--to cloud`
  respeta 5 MiB por archivo: si no entra, va solo el póster (screenshot).
- Página del cliente: en cada entregable `done`, una línea "Probado: tests,
  revisión, 5 recorridos" y el video si existe.

### 3.5 Almacenamiento

- `evidence.max_bytes_per_task` (def. 200 MiB) y `evidence.keep_attempts`
  (def. 3 por tarea): poda al terminar `orch run`; el intento empujado nunca
  se poda mientras la tarea siga abierta.
- Video para el cliente: `evidence.publish.max_video_bytes` (def. 4 MiB).

### 3.6 Privacidad

Todo texto que sale del equipo (comentario, snapshot) pasa por
`findings.Redact` y por `guard.Scan`; si el guard encuentra un secreto, el
campo se descarta entero (no se intenta enmascarar). `ORCH_APP_URL` y rutas
absolutas no salen. Videos y screenshots no se pueden escanear: publicarlos es
**opt-in** (`evidence.publish.video: false` por defecto) y la doc pide datos
de prueba sembrados.

## 4. Claves de config (todas nuevas, `.orchestrator/config.yaml`)

| Clave | Def. | Qué hace |
|---|---|---|
| `evidence.enabled` | `true` si `verify.enabled` | escribe manifiestos |
| `evidence.pr_comment` | `true` | comentario `orch:evidence` |
| `evidence.video` | `retain-on-failure` | valor de `ORCH_E2E_VIDEO` |
| `evidence.keep_attempts` / `evidence.max_bytes_per_task` | `3` / `200MiB` | retención |
| `evidence.publish.video` / `.max_video_bytes` | `false` / `4MiB` | medios en la página del cliente |
| `verify.app.probes[]` | vacío | chequeos de contenido |

## 5. Datos y migraciones

Sin migración SQL: el manifiesto es archivo y los eventos existentes alcanzan.
`verify_passed`/`verify_failed` ganan `extra.evidence` (ruta del manifiesto).
Snapshot `Schema` 1 → 2 (aditivo; `docs/SNAPSHOT-SCHEMA.md` y el Worker de
orch-cloud deben aceptar ambos).

## 6. Fases (un PR cada una)

1. **Manifiesto + `orch evidence`**. Acepta: tras un verify que pasa existe
   `evidence/<task>/evidence.json` con checks, review, diff y E2E; un segundo
   run lo reemplaza y deja el anterior en `history/`. Tests: engine con
   `Verifier` falso (golden del JSON); CLI con testscript.
2. **`verify.app.probes`**. Acepta: app de test que devuelve 200 con
   "not built" es enviada de vuelta con el probe en el feedback. Test visto
   fallar primero contra `waitReady` actual.
3. **Comentario del PR**. Acepta: un solo comentario editado en reintentos;
   un finding con `ghp_…` no aparece. Tests con `gh` falso (patrón de
   `internal/vcs/testdata`).
4. **Retención**. Acepta: con `keep_attempts: 1` quedan solo el manifiesto y
   el directorio E2E del intento empujado. Test de poda en tmpdir.
5. **Snapshot v2 + página del cliente + export**. Acepta: `orch publish --to dir`
   incluye `evidence` y el video ≤ límite; cloud omite el que excede; test de
   que el snapshot no contiene ids ni nombres de modelo. Vitest del componente.
6. **Captura + prueba real**. Receta/skill con `ORCH_E2E_VIDEO`; probado en un
   proyecto web real con `video: on` (criterio de #520), capturas en el PR.

## 7. Riesgos

- Videos pesados: disco y tiempo de E2E (`video: on` ralentiza ~10-30 %).
- Un video puede mostrar datos reales; mitigado por opt-in, no eliminado.
- Comentarios en PR: ruido si cambian a cada poll — solo se editan al empujar.
- Schema 2 rompe un Worker de cloud viejo si valida estricto.

## 8. Preguntas abiertas para Héctor

1. **¿Dónde viven los medios para el link del PR?** Opciones: (a) solo
   local + dashboard con túnel; (b) orch-cloud (R2, límite 5 MiB); (c) rama
   huérfana `orch-evidence` en el repo (engorda el repo). Propuesta: (a) ahora, (b) después.
2. ¿Video en la página del cliente **opt-in** (propuesta) o activado por defecto?
3. ¿Qué video se muestra si hay varios tests: el primero que pasa, o los marcados `@evidence`?
4. ¿Los títulos de pasos (`test.step`) se traducen al idioma del cliente con `snapshot/translate.go`, o se exige escribirlos ya en ese idioma?
5. ¿`orch evidence` como comando nuevo, o basta con el dashboard y `--json` en `orch status`?
