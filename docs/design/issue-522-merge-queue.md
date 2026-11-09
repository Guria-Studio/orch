# #522 — Cola de merge + chequeos semánticos de colisión

Estado: **propuesta para revisión** (no implementado) · Paraguas: #518 ·
Construye sobre #482 (conflict radar) y #438 (conflict retry)

## 1. Problema

Dos tareas paralelas pueden pasar `verify` y CI cada una por su lado y aun así
romper `main` juntas:

- **Base vieja**: `verify` corre una vez, contra la base del momento en que se
  creó el worktree. Si otra tarea entra antes y git mergea limpio, nadie vuelve
  a verificar la combinación (#79/#81: PRs construidos sobre ramas que ya habían entrado).
- **Colisión semántica**: en dogfood, dos tareas crearon **la misma migración
  v5** con nombres de archivo distintos (#99/#103 de ese proyecto). git no ve
  conflicto; el radar por archivos tampoco.

Objetivo: ningún PR se mergea sin pasar `verify` contra el `main` vigente, y
reglas enchufables detectan colisiones de migraciones, endpoints y claves de config.

## 2. Qué existe hoy (código)

| Pieza | Dónde | Comportamiento |
|---|---|---|
| Verify | `internal/engine/verifygate.go:64-76` | una vez por intento, antes del push, contra `origin/<base>` del worktree |
| Auto-merge | `internal/engine/cipoll.go:321-375` | mergea cada PR en verde apenas su CI pasa; `gh pr merge --squash [--auto]` (`internal/vcs/github.go:350-369`) |
| Orden de merge | `cipoll.go:160-166` + `internal/state/ci.go:60-64` | el del poll: `ORDER BY task_id`. No hay prioridad ni re-verify |
| PR atrasado | `cipoll.go:341-357` | si GitHub actualiza la rama (`BranchUpdated`), espera CI de nuevo; `verify` local no corre |
| Conflict retry (#438) | `cipoll.go:645-718` | merge de la base en el worktree; si es limpio **empuja sin verify** (`:668-688`); si no, al agente (`retry.conflict`, `internal/config/config.go:267-270`) |
| Land local (#455) | `internal/worktree/land.go:48-103`, `reaper.go:396-422` | aterriza de a una con `merge-tree`; si la base se movió limpio, **sin re-verify** |
| Radar pre-dispatch | `internal/engine/radar.go:43-72` | `files` declarados que se pisan → `files_overlap` (warn/serialize) |
| Radar post-PR | `radar.go:139-169`, `land.go:107-110` | `merge-tree` contra los otros PRs **con CI pendiente** (ponytail `:136-138`: se escapan los PRs verdes esperando a una persona) → `branch_conflict` |
| Prioridad de tareas | `internal/model/task.go:15-35`, `internal/engine/queue.go:249-266` | no hay campo `priority`; `Ready` ordena por (fase, id) |
| Precedente en orch | `internal/state/migrate.go:43-74` | orch rechaza migraciones no contiguas **al arrancar**: el caso v5/v5 en orch rompería después del merge, no antes |
| Eventos | `internal/state/migrations/001_init.sql:69-81`, `internal/state/events.go:34` | `events(event_type, task_id, ts, extra_json)`; `AllEvents` ordena en Go |

## 3. Diseño

### 3.1 Cola de merge (`merge_queue`)

Sustituye el "mergear apenas está verde" de `ciSucceeded` cuando
`merge_queue.enabled` (y en el land local). Una tarea entra a la cola cuando su
PR está verde (o, en local, cuando su verify pasó). Se procesa **de a una**:

1. Tomar la cabeza según el orden (§3.2).
2. `Worktree.Recreate` + `MergeBase` contra la base **actual** (ya existen).
   Conflicto → camino de #438 sin cambios; la tarea sale de la cola y vuelve a
   entrar cuando esté verde.
3. **Chequeos semánticos** (§3.3) de la rama contra la base actual. `block` →
   sale de la cola y vuelve al agente con el hallazgo en el feedback.
4. **Re-verify** con el mismo `Verifier` (checks + E2E; la review LLM no se
   repite, `merge_queue.review: false`) si la base se movió desde el último
   verify de la tarea (`base_sha` del intento ≠ base actual). Falla → al agente,
   como `sendBack`, con tope `merge_queue.max_requeues`.
5. Empujar si el merge movió la rama; esperar CI verde del nuevo head.
6. Mergear y comprobar que la base que entró es la verificada; si alguien
   mergeó por fuera entre medio, volver al paso 2. Local: `LandLocally` ya
   rechaza si la base se movió (`land.go:90-97`).

Además, independiente de la cola, se cierran los dos agujeros actuales: el
push limpio de `ciConflict` y el land local con base movida pasan por verify.

### 3.2 Orden

Clave, en este orden: **fase** ascendente → `priority` de la tarea (campo
opcional nuevo en `tasks.json`, mayor primero, sin él = 0) → **cantidad de
dependientes** (transitivos) descendente, para liberar antes lo que destraba
más trabajo → `enqueued_at`. Las dependencias ya garantizan que un dependiente
no se despacha antes que su dependencia esté mergeada (#347).

### 3.3 Chequeos semánticos enchufables

Paquete nuevo `internal/collide`, sin dependencias nuevas, que lee árboles con
`git ls-tree`/`git diff --name-status` y solo mira lo que **la rama agrega**
(así una regla no salta por lo que ya estaba en la base):

```go
type Check interface {
    Name() string
    // Collisions compara lo que head agrega frente a base y frente a others
    // (las otras ramas abiertas o en cola).
    Collisions(ctx context.Context, repo string, base, head string, others []string) ([]Collision, error)
}
type Collision struct{ Rule, Key, File, With string } // With: otra rama/tarea, o "base"
```

Tipos de regla, en `.orchestrator/config.yaml` (orch no lee un `orch.yaml`):

```yaml
semantic_checks:
  - name: migrations
    kind: sequence              # identidad = grupo 1 del regex sobre el NOMBRE de archivos agregados
    paths: ["db/migrations/*.sql", "internal/state/migrations/*.sql"]
    key: '^(\d+)_'
    contiguous: true            # además, el número nuevo debe ser max(base)+1
    mode: block                 # block | warn | off
  - name: endpoints
    kind: unique_key            # identidad = grupo 1 sobre las LÍNEAS agregadas del diff
    paths: ["internal/**/*.go"]
    pattern: '"(?:GET|POST|PUT|PATCH|DELETE) (/[^"]*)"'
    mode: warn
  - name: config-keys
    kind: unique_key
    paths: ["internal/config/config.go"]
    pattern: 'yaml:"([a-z_]+)"'
    mode: warn
  - name: custom
    kind: command               # escape: exit ≠ 0 = colisión; stdout = detalle
    run: scripts/check-collisions.sh   # recibe ORCH_BASE_SHA, ORCH_HEAD_SHA, ORCH_OTHER_HEADS
```

`sequence` atrapa el caso v5/v5 de dos formas: contra la base (A ya mergeó
`005_a.sql`; B agrega `005_b.sql` → clave `005` existe) y contra otra rama
abierta (A y B abiertas, ambas agregan `005_*`). `unique_key` busca la clave
agregada en el contenido de la base (`git grep` en el árbol) y en las líneas
agregadas por las otras ramas.

Dónde corren: **al abrir el PR**, junto a `radarPRs` (`reaper.go:378`), contra
las otras ramas abiertas → evento `semantic_collision`, siempre aviso; y **en
la cola** (paso 3) contra la base vigente → respeta `mode`. Sin cola, el paso 3
corre en `ciSucceeded` antes de `MergePR`.

### 3.4 Relación con #482 y #438

- **Radar = predicción** (antes de despachar y al abrir el PR); **cola =
  garantía** al mergear. El radar sigue en `warn` por defecto.
- El radar post-PR pasa a comparar también contra las entradas de la cola, lo
  que cierra su ponytail (PRs verdes esperando).
- **#438** queda como el manejador de conflictos de la cola, sin duplicarlo;
  solo gana el verify tras el merge limpio.

### 3.5 Medir el radar (`orch radar report`)

Fuente: `events` de `orch.db` vía `AllEvents` (fechas con `state.ParseTS`).

- **Predicciones**: `files_overlap` y `branch_conflict` (par tarea/`with`, archivos).
- **Choques reales**: `ci_conflict_retry` (PR y land local) y, nuevo,
  `semantic_collision` en modo bloqueo y `queue_ejected`.
- **Acierto** (recall) = choques reales de la tarea T con una predicción previa
  para T con archivos en común / choques reales. **Precisión** = pares predichos
  que después chocaron / pares predichos. **Escapes** = choques sin predicción.
- Hoy `ci_conflict_retry` no dice con qué tarea chocó: se agrega
  `extra.caused_by` (ids de tarea de los commits de la base que causaron el
  conflicto; `prompt.Conflict.Commits` ya trae los subjects).
- Salida: tabla por defecto, `--since 30d`, `--json`. Es el "informe de
  efectividad" del criterio de aceptación y la línea base antes de activar la cola.

## 4. Claves de config (nuevas)

| Clave | Def. | Qué hace |
|---|---|---|
| `merge_queue.enabled` | `false` | activa la cola (requiere `github.auto_merge` o `vcs.provider: local`) |
| `merge_queue.reverify` | `when_base_moved` | `always` \| `when_base_moved` \| `never` |
| `merge_queue.review` | `false` | repetir la review LLM en el re-verify |
| `merge_queue.max_requeues` | `2` | veces que la cola devuelve una tarea al agente antes de bloquearla |
| `semantic_checks[]` | vacío | reglas §3.3 |
| `priority` (en `tasks.json`) | `0` | desempate de orden; `orch validate` y el atomizer lo aceptan |

## 5. Datos y migraciones

`016_merge_queue.sql`: tabla `merge_queue(project_id, task_id, pr_url,
enqueued_at, state CHECK IN ('queued','testing','merged','ejected'), base_sha,
head_sha, requeues, reason, PRIMARY KEY(project_id, task_id))` — la cola
sobrevive a un reinicio de `orch run` (hoy el estado de merge es en memoria,
`cipoll.go:418-425`). Timestamps con `state.FormatTS`. Eventos nuevos
(registrados en `internal/state/eventtypes_test.go`): `queue_enqueued`,
`queue_merged`, `queue_ejected`, `semantic_collision`.

## 6. Fases (un PR cada una)

0. **`orch radar report`** + `caused_by` en `ci_conflict_retry`. Acepta:
   fixture de `orch.db` con 2 predicciones y 3 choques → recall 1/3, precisión
   1/2, JSON estable. Solo lectura; da la línea base.
1. **Verify tras merge limpio** en `ciConflict` y en land local. Acepta: test
   de integración donde la base se mueve limpio con un cambio que rompe el test
   de la tarea → la tarea vuelve al agente (hoy se empuja/aterriza). Visto fallar primero.
2. **`internal/collide` + `sequence` + config**. Acepta: test que reproduce
   v5/v5 (repo temporal, dos ramas con `005_a.sql`/`005_b.sql`, una mergeada)
   → colisión `migrations/005`; y con ambas abiertas → `semantic_collision` al
   abrir el PR. Modo `block` en `ciSucceeded` impide el merge.
3. **`unique_key` y `command`**. Acepta: endpoint `GET /api/tests` duplicado
   detectado; clave ya presente en la base no salta si la rama no la agrega.
4. **Cola** (migración 016, orden, proceso serial, GitHub y local). Acepta:
   tres PRs verdes se mergean en el orden fase/priority/dependientes; uno cuya
   base se movió se re-verifica; reinicio a mitad de cola retoma. Tests con
   provider `gh` falso y `Verifier` falso.
5. **Dashboard + docs**: sección de cola en Delivery, `docs/CONFIG.md`,
   `docs/CLI.md`, manuales.

## 7. Riesgos

- **Throughput**: serial + re-verify con E2E puede tardar minutos por PR.
  `when_base_moved` evita la mayoría; batching queda fuera de esta versión.
- **E2E inestables** expulsan tareas sanas; contar `flaky` como pase.
- **Merge queue nativa de GitHub** u otras reglas de protección: si el repo la
  usa, orch no debe competir (pregunta 3).
- Regex de `unique_key` con falsos positivos: por eso `warn` por defecto y
  solo líneas agregadas (lección del guard de secretos, #518).

## 8. Preguntas abiertas para Héctor

1. **Prioridad**: ¿campo `priority` nuevo en `tasks.json`, o alcanza con fase
   + cantidad de dependientes?
2. ¿Las reglas viven en `.orchestrator/config.yaml` (propuesta) o se quiere un
   archivo propio versionado en el repo (`.orch/collisions.yaml`)?
3. Si el repo tiene la **merge queue de GitHub** activa: ¿orch la detecta y la
   delega (solo chequeos semánticos), o se ignora?
4. ¿`orch init`/templates agregan una regla `migrations` por defecto para
   stacks conocidos (Supabase, Prisma, Django, Alembic — este usa
   `down_revision`, no números)?
5. ¿El re-verify de la cola repite la review LLM alguna vez (cuesta tokens), o
   nunca?
6. ¿Dónde está el incidente v5/v5 (proyecto y PRs) para usarlo como fixture
   real además del sintético?
