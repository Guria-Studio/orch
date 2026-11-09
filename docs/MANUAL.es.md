# orch — Manual de usuario paso a paso (CLI en Go)

> También disponible en: [English](MANUAL.en.md) · [Português](MANUAL.pt.md)
>
> Este manual documenta el binario **Go** de `orch`, la reescritura como
> binario único. Cada comando de abajo es la salida real de `orch --help` sobre
> un binario compilado; la referencia completa de flags está en
> [`CLI.md`](CLI.md), que es la fuente de verdad con la que este manual se
> mantiene sincronizado. ¿Buscas el `orch` antiguo en Python? Mira
> [Python legacy](#python-legacy) al final.

**orch corre agentes de IA como un equipo y le muestra a tu cliente una página
en vivo, no un hilo de estado.** Es para freelancers y agencias que construyen
con IA: divide el trabajo en un grafo de tareas, corre cada una en la CLI de
agente de código a la que la ruteas, en paralelo, y mantiene una página que tu
cliente puede abrir para ver cómo va el trabajo. ¿Sin cliente? La misma página
es tu propio tablero de estado.

```bash
curl -fsSL https://raw.githubusercontent.com/Guria-Studio/orch/main/scripts/install.sh | sh
cd my-project                        # a git repository
orch start "what you want to build"
```

`orch start` configura el proyecto con las CLIs de IA que encuentra, deja que
claude planifique la idea contigo, estima el plan y corre las primeras tareas
con el dashboard abierto. El resto de este manual es ese mismo camino paso a
paso, y lo que viene después: `orch run` para las tareas que quedan,
`orch dashboard` para seguir y revisar el trabajo, y `orch publish` para la
página del cliente.

**Tiempo total de setup**: ~2 minutos (un `curl | sh`, sin Python, sin venv).
**Tiempo por feature**: unos minutos escribiendo el spec + ejecución desatendida.

---

## Contenido

1. [Instalación](#1-instalación)
2. [Crear un proyecto nuevo](#2-crear-un-proyecto-nuevo)
3. [Escribir specs y atomizarlos en tasks.json](#3-escribir-specs-y-atomizarlos-en-tasksjson)
4. [Revisar el plan antes de correrlo](#4-revisar-el-plan-antes-de-correrlo)
5. [Run — despachar tareas a los agentes de IA](#5-run--despachar-tareas-a-los-agentes-de-ia)
6. [Abrir el dashboard](#6-abrir-el-dashboard)
7. [Inspeccionar una corrida: status, tasks, events, logs](#7-inspeccionar-una-corrida-status-tasks-events-logs)
8. [Arreglar cosas a mano: task set, task-status, reset](#8-arreglar-cosas-a-mano-task-set-task-status-reset)
9. [El router de modelos](#9-el-router-de-modelos)
10. [Inspeccionar la config](#10-inspeccionar-la-config)
11. [MCP — que un agente maneje orch directamente](#11-mcp--que-un-agente-maneje-orch-directamente)
12. [Instalar los skills de orch](#12-instalar-los-skills-de-orch)
13. [Cuando algo falla](#13-cuando-algo-falla)
14. [Actualizar orch](#14-actualizar-orch)
15. [Funciones por capa](#15-funciones-por-capa)
16. [Python legacy](#python-legacy)

---

## 1. Instalación

```bash
curl -fsSL https://raw.githubusercontent.com/Guria-Studio/orch/main/scripts/install.sh | sh
```

Descarga el tarball correcto para `linux`/`darwin` × `amd64`/`arm64` desde el
último GitHub Release, verifica su checksum e instala en `~/.local/bin`
(`INSTALL_DIR=...` para cambiarlo). Sin toolchain de Go, sin Python, sin
dependencias que resolver: `orch` es un binario estático.

O desde el código, solo la CLI (`go install github.com/Guria-Studio/orch/cmd/orch@latest`, Go ≥ 1.25): se compila sin los bundles web, así que `orch dashboard` y `orch publish` necesitan el script o un tarball del release.

O descarga un tarball directamente de
[la página de Releases](https://github.com/Guria-Studio/orch/releases) y pon
`orch` en tu `PATH` a mano. Mira [`RELEASING.md`](RELEASING.md) para el esquema
completo de tags y cómo se construyen los releases.

Verifica:

```bash
orch --version
orch --help
```

`orch --help` lista los comandos por capa: *Start a project*, *Run tasks*,
*Follow progress*, *Deliver results*, *Integrations*, *Advanced*.

### CLIs de IA que necesitas

Al menos **una** de estas en tu `PATH`, autenticada con una suscripción o una
API key. orch despacha a las seis, y un mismo proyecto puede mezclarlas: cada
tarea va a la CLI a la que rutea su modelo ([§9](#9-el-router-de-modelos)):

- **`claude`** — Claude Code CLI (Anthropic)
- **`codex`** — Codex CLI (OpenAI)
- **`gemini`** — Gemini CLI (Google)
- **`opencode`** — OpenCode (cualquier proveedor que tenga configurado)
- **`agy`** — Antigravity CLI (Google; varias familias de modelos detrás de una sola CLI)
- **`muse`** — Muse Code (Meta). orch lo corre sin aprobaciones y sin su
  sandbox (`--approval-mode never --disable-sandbox`), para que el agente
  pueda reportar su tarea terminada

Solo necesitas las CLIs a las que rutean tus tareas.

Corre `orch doctor` cuando quieras para ver qué está realmente disponible;
mira [§13](#13-cuando-algo-falla).

---

## 2. Crear un proyecto nuevo

**El camino corto**: en un repositorio git, `orch start "lo que quieres construir"`
hace toda esta sección y la §3 por ti: crea el proyecto con las CLIs de tu
PATH, instala los skills de planificación, deja que claude planifique la idea
contigo (`/orch-plan`), valida y estima el plan, y ofrece correr las dos
primeras tareas con el dashboard abierto. El resto de esta sección es el mismo
camino paso a paso.

```bash
orch init ~/work/my-app --template python-api
```

```
$ orch init --help
Scaffold a new orch project.

With no arguments, asks three questions (project id, template, confirm) and shows everything it is about to write before writing any of it; --advanced asks the rest.
The project goes in the current directory, with the default models of the AI CLIs on PATH.
With a path or any of the flags below, scaffolds directly.

Templates: chatbot-whatsapp, data-pipeline, expo-mobile, nextjs-saas, python-api

Usage:
  orch init [PATH] [flags]

Flags:
      --advanced              Also ask the project root, state backend, budget preset, spec root, tier models and openspec layout
      --force                 Overwrite an existing project's files
  -h, --help                  help for init
      --no-permissions        Do not allow orch's agent rules in the settings of the CLIs the project uses; orch claude-permissions / orch agy-permissions add them later
      --no-report-findings    Write report_findings.enabled: false (it is on by default: agents queue the bugs they find in orch, and you review each before it is sent)
      --project-name string   Name used in generated files; default = the directory name
      --sdd                   Also scaffold the openspec/ layout
      --template string       Project template (chatbot-whatsapp, data-pipeline, expo-mobile, nextjs-saas, python-api); omit for a blank project
```

Córrelo **sin argumentos**, en el directorio de tu proyecto, y hace tres
preguntas: project id (por defecto, el nombre del directorio), template (por
defecto, blank) y la confirmación. Con Enter en todas te queda un proyecto que
corre. Escribe en el **directorio actual**, elige los modelos por defecto entre
las CLIs de IA que encuentra en el `PATH` (primero claude, después codex,
gemini, opencode, agy, muse) y escribe la rama de tu repositorio como
`dispatch.base_branch`. `orch init --advanced` pregunta además el root, el
backend de estado, el preset de presupuesto, el spec root, los modelos por tier
y el layout openspec.

```bash
orch init
```

Eso escribe `.orchestrator/config.yaml`, `.orchestrator/model_router.yaml`,
`tasks.json` y (con `--sdd`) un layout `openspec/`. También escribe un
`.mcp.json` que apunta a `orch mcp` (mira [§11](#11-mcp--que-un-agente-maneje-orch-directamente)),
que se deja intacto en un nuevo `init`, `--force` incluido, porque puede que ya
liste otros servidores MCP del proyecto.

Un agente reporta su tarea con `scripts/task-finish.sh` /
`task-block.sh` y las herramientas MCP de orch, y una CLI que corre headless
rechaza lo que no tiene una regla. Por eso `orch init` y `orch start`
**otorgan por defecto los permisos básicos de orch** a cada CLI a la que rutea
el proyecto y que está en tu `PATH`: nunca a una que no usa, y solo esas reglas
acotadas, nunca un flag para saltarse los permisos. Las de claude van en el
`.claude/settings.json` del proyecto; las de agy, en
`~/.gemini/antigravity-cli/settings.json` (fuera del proyecto, así que imprime
la ruta y cada regla agregada). El wizard pregunta y Enter acepta; `init` en
modo batch y `start --yes` las escriben directamente. `--no-permissions` no
escribe nada y nombra `orch claude-permissions` / `orch agy-permissions`, que
las agregan después. codex, opencode y muse no necesitan nada escrito;
`orch doctor` informa de cada CLI (gemini como desconocida). gemini rechaza una
carpeta que no le dijeron que es de confianza, y cada tarea corre en un
worktree nuevo, así que orch define `GEMINI_CLI_TRUST_WORKSPACE=true` en cada
despacho de gemini; ponla en `false` en tu entorno para mantener ese control.

Un proyecto nuevo trae `report_findings.enabled: true`: el wizard y
`orch start` lo preguntan una vez, con sí por defecto. Los agentes que
encuentran un bug en el propio orch **encolan** un reporte; nada se publica
por su cuenta. Lees cada uno, con el título y el cuerpo tal como se publicarían
en Guria-Studio/orch con tu login de `gh`, con `orch findings` o en la página
System › Findings del dashboard, y lo envías (`orch findings send ID`) o lo
descartas. `orch run` y Now dicen cuántos esperan. `--no-report-findings` lo
apaga. Mira [`DOGFOODING.md`](DOGFOODING.md).

Un proyecto en blanco empieza con **una tarea de ejemplo**, `F0.T1` ("Add a
Getting started section to README.md"), en el modelo más barato de una CLI
instalada, para que el primer `orch run` muestre a un agente trabajando. Cada
tarea parte de un commit, así que primero haz commit de lo que escribió init;
init lo avisa cuando el repositorio no tiene ninguno:

```bash
git add -A && git commit -m "orch init"
orch run
```

Sin remote de git, el trabajo queda en la rama local `orch/<task-id>` de cada
tarea: sin push y sin pull request.

Un proyecto con template (`--template`) viene con tareas reales y ruteadas en
`tasks.json`: `orch tasks` funciona de inmediato, sin atomizar nada para probar
el resto de este manual.

---

## 3. Escribir specs y atomizarlos en tasks.json

Los specs son archivos markdown bajo `<project-root>/docs` (o donde apunte
`spec_root` en `config.yaml`) con el formato descrito en
[`SPEC-FORMAT.md`](SPEC-FORMAT.md). `orch atomize` los parsea y fusiona las
tareas nuevas en `tasks.json`, **en solo lectura salvo que pases `--apply`**:

```bash
$ orch atomize --help
Parse markdown specs and merge them into tasks.json (read-only unless --apply)

Usage:
  orch atomize [flags]

Flags:
      --apply               Escribí tasks.json (con backup). Sin este flag es read-only.
      --file string         Sólo un archivo markdown (bypass del walk de --specs-dir)
  -h, --help                help for atomize
      --list                Modo listar: sólo imprime lo parseado, sin merge/diff
      --no-backup           No crear backup .bak-<ts> al escribir (default: sí crea)
      --specs-dir string    Directorio de specs .md (default: <project-root>/<spec_root>, "specs")
      --tasks-json string   Path a tasks.json (default: <project-root>/tasks.json)
```

(El texto de ayuda de los flags está en español en el binario hoy: es la salida
real, no un error de este manual.)

```bash
# Vista previa: parsea cada spec del directorio de specs, muestra un diff, no escribe nada
orch atomize

# Un solo archivo
orch atomize --file specs/f1-auth.md

# Aplicar: escribe tasks.json (con un backup tasks.json.bak-<ts> salvo --no-backup)
orch atomize --file specs/f1-auth.md --apply
```

**Garantías**: es idempotente (volver a correrlo con el mismo spec no toca los
IDs de tareas existentes, solo agrega los nuevos); una tarea que nombra un
modelo que no está en `model_router.yaml` la detecta `orch validate` o el
despacho, no se acepta en silencio; el parser se salta los bloques de código
con fences de un spec (así un spec de ejemplo dentro de `specs/README.md` no se
importa como tareas reales).

**Las tareas nuevas quedan en `backlog`**, y `orch run` nunca despacha una
tarea en backlog. `--apply` termina diciéndolo, con el comando que las
promueve — `orch task set --id F1.1.T1 --status todo` para una, un bucle
`for id in …; do …; done` sobre el mismo comando para varias. Promueve las que
quieras correr. Si te olvidas, `orch run` (y `orch estimate`) dicen cuántas
tareas siguen en backlog e imprimen el mismo comando, en vez de solo "nothing
is ready".

---

## 4. Revisar el plan antes de correrlo

El comando `run` del binario en Go **no tiene flag `--dry-run`**: es una
carencia conocida y documentada respecto de Python (mira la fila de `run` en
[`CLI.md`](CLI.md)). Para ver qué haría una corrida, usa los comandos de solo
lectura:

```bash
# Todo lo que está mal en el DAG o el ruteo, antes de gastar un token
orch validate

# Qué está listo para despachar ahora mismo
orch tasks --status todo

# El grafo de dependencias, como Graphviz DOT
orch graph | dot -Tpng -o plan.png

# Qué despacharía la próxima corrida, y cuánto costaría y tardaría
orch estimate
```

`orch validate` corre los checks del preflight de Python que ya tienen un lugar
en Go: carga de config/router/tasks, schema, ciclos de dependencias y rutas de
modelo sin resolver. Exit code `0` si está limpio, `2` si encontró al menos un
error.

```bash
$ orch validate --help
Static validation of tasks.json + routing (schema, deps, cycles, routes)

Usage:
  orch validate [flags]

Flags:
      --files   Also check that parent dirs of each task.files[] entry exist + are writable (not implemented yet)
  -h, --help    help for validate
      --json    Emit the full validation report as JSON on stdout
```

Corre también `orch doctor` aquí (mira [§13](#13-cuando-algo-falla)); cubre el
lado del entorno que `validate` no revisa: CLIs de proveedor realmente en el
`PATH`, estado del VCS, worktrees huérfanos, sensatez del preset de
presupuesto, salud de SQLite.

---

## 5. Run — despachar tareas a los agentes de IA

```bash
$ orch run --help
Walk the DAG, dispatching ready tasks to their CLI agents

Usage:
  orch run [flags]

Flags:
  -h, --help            help for run
      --max-tasks int   Start at most this many tasks (their retries do not count); 0 means no limit
      --mode string     auto dispatches everything ready; semi asks before each critical task (default "auto")
      --no-push         Skip pushing task branches — for a project with no remote
      --only string     Only dispatch tasks whose id matches this glob (dependencies still resolve across the whole DAG)
      --task-locks      Take a per-task lock, so several orch instances can share one project
      --worktree-mode   Give each task its own git worktree and branch (also settable as dispatch.worktree_mode)
```

```bash
# Despacha todo lo que esté listo, sin preguntar
orch run

# Pregunta antes de cada tarea marcada como crítica
orch run --mode semi

# Aísla cada tarea en su propio git worktree/rama y abre un PR por tarea
# si termina bien (necesita dispatch.worktree_mode / vcs.auto_pr — mira CONFIG.md)
orch run --worktree-mode

# Todavía no hay remote al que hacer push
orch run --no-push
```

**Ctrl-C** termina el trabajo en curso antes de salir, con exit code **130**.
Un **segundo** Ctrl-C manda SIGKILL de inmediato a todos los grupos de procesos
hijos.

**Modo worktree**: cada tarea recibe su propio git worktree y su rama, en
`<project>.worktrees/<task-id>/` junto al proyecto (mira `CONFIG.md`); si
termina bien, `orch` hace commit, push y, si `vcs.auto_pr` está activo, abre un
PR hacia `dispatch.base_branch`, y deja la tarea en `in-progress` para el
poller de CI. Un check de CI verde la marca `done` (y también la fusiona, si
`github.auto_merge` está activo: un merge rechazado porque la base acaba de
moverse se reintenta durante un par de minutos, y un PR atrasado respecto de su
base se actualiza primero, así las dependientes parten de una base que tiene el
código; un merge que nunca entra se informa con el error del propio gh en el
evento `pr_auto_merge_failed` y en la nota de la tarea); uno rojo recibe un
reintento con los logs del fallo adjuntos y después pasa a `blocked`. Un rojo
causado por GitHub al no arrancar nunca el job (billing de Actions, el límite
de gasto) se bloquea de inmediato con esa razón, sin gastar el reintento. Un PR
que choca con su base se pone al día sobre el trabajo guardado de la tarea:
orch fusiona la base y hace push si la fusión es limpia, sin agente; si no, el
agente recibe los archivos en conflicto y los commits de la base que los
causaron para resolverlos, `retry.conflict` veces (1 en un proyecto nuevo; una
config sin la clave sigue bloqueando como antes) antes de que la tarea se
bloquee nombrando los archivos. Una corrida que abre un PR termina antes de
consultar el CI de ese PR: una corrida que termina en verde se nota en el
*siguiente* `orch run`, igual que en Python.

**Conocimiento del proyecto** (`knowledge.enabled`, activo en un proyecto
nuevo): lo que aprende una tarea llega a las que vienen después. Las lecciones
son archivos markdown cortos bajo `.orch/knowledge/`, que se commitean y se
revisan como cualquier otro archivo. Un agente registra una con la herramienta
MCP `orch_learn` (queda en su PR); orch redacta una cuando el verify local
rechazó un intento y el siguiente pasó, o cuando el agente resolvió un PR en
conflicto; tú agregas las tuyas con `orch knowledge add`. Cada prompt de
despacho lleva las pocas más relevantes bajo `Project knowledge`, primero las
cuyo `area` cubre los archivos de la tarea. `orch knowledge list` / `show` /
`rm` las administran y `orch knowledge stats` compara el éxito al primer
intento antes y después de la primera lección. Mira
[`CONFIG.md`](CONFIG.md#knowledge--new-451).

**Verify local con la app de la tarea** (`verify.app`): orch arranca la app en
el worktree de la tarea, con el `.env` de la tarea, antes de correr los tests
E2E, una vez por cada verify de cada tarea. Una app que migra su base de datos
al arrancar, inicia jobs programados o llama a proveedores reales lo hace cada
vez. Así que apúntala a una base de datos y a servicios que solo use esa tarea.
`dispatch.isolate_compose` aísla solo los contenedores dentro del proyecto
compose de la tarea, no una base de datos a la que el `.env` apunte fuera de
él. Mira [`CONFIG.md`](CONFIG.md#verify--new).

**Concurrencia, presupuestos y reintentos** se manejan con la config, no con
flags; mira [`CONFIG.md`](CONFIG.md).

**La ventana de presupuesto es por proyecto.** `budgets.yaml` cuenta los
dispatches de este proyecto, ponderados (lecturas de caché al 10%, escrituras
al 125%), contra un `token_budget` que tú calibras; no es la cuota de tu plan,
que comparten otros proyectos y tu propia terminal. claude y codex sí reportan
las ventanas reales del plan, de 5 horas y semanal — claude en su stream, codex
en el rollout de su sesión en `~/.codex/sessions/` (`$CODEX_HOME`), que orch lee
después de cada dispatch a codex — y la página Providers and quota del
dashboard y `orch report usage` las muestran. Escribe `source: provider` bajo un
proveedor en `budgets.yaml` (o `orch budget set codex --source provider`) para
pausar al `threshold_pct` de esas ventanas; mientras no haya un reporte vigente
decide la ventana de tokens. `orch budget show` dice, por proveedor, cuál decide.

**Qué modelo corrió de verdad.** `codex exec` no lo dice; el rollout de su
sesión sí. orch guarda, por intento, el modelo y el esfuerzo con que corrió
codex (y `default` cuando codex descartó un service tier que el modelo no
ofrece) junto a los de la ruta: `orch status --json` lo muestra como
`effective`, la tabla agrega `(ran <modelo>)`, `orch metrics` cuenta los
intentos que corrieron otra cosa y cada uno escribe un evento `model_mismatch`
(`orch events <tarea>`). Lo que no se reportó queda desconocido.

### Empezar sin GitHub

Un repositorio que todavía no tiene remote, o una máquina sin `gh`, no necesita
pull requests para llevar el trabajo a la rama base. Pon `vcs.provider: local`
(`orch init` lo escribe por ti en ese caso; `orch start` también, una vez que
el repositorio tiene un commit):

```yaml
dispatch:
  worktree_mode: true
vcs:
  provider: local
```

Cada tarea sigue trabajando en su propia rama, en su propio worktree. Cuando
termina y pasa el verify local, orch **la fusiona con squash en
`dispatch.base_branch` aquí mismo**: un commit por tarea, con el título que
tendría el PR, y borra la rama. Las tareas se fusionan de una en una, y una
dependiente solo se despacha cuando sus dependencias ya están en la base, así
que parte de su trabajo. No se hace push y `gh` nunca se ejecuta. Una tarea
cuyo trabajo choca con la base recibe la base fusionada en su worktree y vuelve
a su agente para resolver el conflicto, `retry.conflict` veces; después se
bloquea nombrando los archivos (su trabajo queda en `orch/<task-id>`). Si la
rama base está en checkout con cambios sin commit en los mismos archivos, la
fusión se rechaza y la tarea se bloquea: haz commit o stash y luego
`orch task-status <id> todo`.

`orch status` muestra `merged_locally` como último evento de la tarea, el
dashboard dice "Fusionada localmente" donde iría el enlace al PR y el recibo de
la corrida lista las fusiones. `orch doctor` no pide remote ni `gh`.

**Pasar a GitHub más adelante**: crea el repositorio, `git remote add origin
<url>`, `git push -u origin main` y pon `vcs.provider: github` (y
`gh auth login`). A partir del siguiente `orch run`, cada tarea abre un PR como
siempre.

---

## 6. Abrir el dashboard

```bash
$ orch dashboard --help
Serve the operator dashboard on a local HTTP port.

Shows the project's state. From this machine the operator can also
act on it — edit and steer tasks, accept or send back their work,
change config, budgets and lessons — through the same functions as
the CLI commands; it never drives orch run. The
client profile (`stakeholder` in config) gates every data route
behind a token and an allow-list, so your client sees only the
client page — see `profile` and `token` under `dashboard:` in
config.yaml, which the flags below override.

Usage:
  orch dashboard [flags]

Flags:
  -h, --help             help for dashboard
      --host string      Address to bind; beyond localhost (0.0.0.0) every remote request needs the token (default "127.0.0.1")
      --port int         Port to listen on; 0 picks any free one (default 7420)
      --profile string   Access profile: operator, stakeholder or both (default: config.yaml)
      --token string     Shared token a client session must present (default: config.yaml)
      --tunnel           Also start the Cloudflare quick tunnel (needs tunnel.enabled and cloudflared), and stop it on exit

Global Flags:
      --config string         Path to config.yaml (default: .orchestrator/config.yaml)
      --project-id string     Project id override. Env fallback: ORCH_PROJECT_ID.
      --project-root string   Project root; default = cwd. Env fallback: ORCH_PROJECT_ROOT.
```

```bash
# Todo, para ti
orch dashboard

# Un proyecto sintético con historial realista, para recorrerlo primero
# (no despacha nada; los datos se borran al detenerlo)
orch dashboard --demo

# La última corrida terminada en Markdown, para pegar en un PR o un chat: tareas
# hechas y bloqueadas, tiempo real y de agentes, gasto por proveedor, PRs (--json para scripts)
orch report receipt

# La misma corrida en palabras simples: qué se construyó, por qué se detuvo cada
# tarea bloqueada y los próximos pasos con su comando (orch run lo imprime al terminar)
orch report summary

# Un enlace para darle a un cliente, por un túnel rápido de Cloudflare (necesita
# `tunnel: enabled: true` en config.yaml y cloudflared en el PATH — la
# página Share muestra cómo instalarlo). Imprime el enlace a la página del cliente,
# con el token que exige cada petición por el túnel; Ctrl+C lo revoca.
# La página Share también puede compartir por Tailscale (serve o Funnel).
orch dashboard --tunnel
```

El dashboard tiene cuatro destinos, más **System** (herramientas, proveedores
y cuota, skills, knowledge, findings, ajustes) aparte. **Now** es la primera
pantalla: la corrida, cada agente trabajando con su reloj en marcha, las tareas
bloqueadas y las que esperan una ventana de presupuesto (con el comando
que desbloquea cada una y, desde esta máquina, los botones que lo hacen), las fases y la
ventana de presupuesto; cuando una corrida termina, su recibo con la corrida en
palabras simples debajo: por qué se detuvo cada tarea bloqueada y los próximos
pasos con sus comandos. **Work** reúne List, Kanban, Graph, Phases y Pace como
pestañas que comparten un mismo conjunto de filtros en la URL; **Usage** reúne
tokens y gasto por modelo, tarea e intento, y el presupuesto; **Delivery** reúne
la página del cliente, el resumen para el cliente, CI, Tests
(cada corrida E2E que hizo el verify local, con lo que dejó un test que falló:
error, paso, captura, video, traza) y Share (un túnel rápido de Cloudflare o Tailscale). **Logs** se abre como
panel sobre cualquier página. Las direcciones antiguas (`/kanban`, `/budget`,
`/tunnel`…) redirigen a su nuevo lugar. **⌘K** (Ctrl+K) busca páginas, tareas
y filtros y copia comandos de la CLI; `g` y luego `n`/`w`/`c`/`d` cambia de
destino, `/` busca en la lista de tareas y `?` muestra todos los atajos. La
página de una tarea muestra su descripción como Markdown: código en línea, y
bloques con su lenguaje, colores de sintaxis y un botón para copiar (Editar
sigue mostrando el texto sin formato).

Su pestaña **Cambios** es la revisión: lo que cambió la rama `orch/<id>` de la
tarea respecto de su base (`git diff --merge-base`), como lista de archivos con
contadores +/− y hunks con colores de sintaxis; un archivo con más de 400
líneas cambiadas se abre plegado. Al lado, cada vez que el verify local devolvió
el trabajo, y los hallazgos de la revisión bajo el `archivo:línea` que nombran.
El operador local actúa desde ahí: **Aceptar** fusiona con squash el PR de la
tarea (`orch task accept`), **Devolver** reabre la tarea con tu nota para su
agente (`orch task note --retry`), **Bloquear** la detiene con la nota como
motivo. En modo local (`vcs.provider: local`) no hay PR: Aceptar hace commit de
lo que la tarea dejó en su worktree y fusiona con squash `orch/<id>` en la base
aquí, como hace la corrida, y luego marca la tarea como hecha; si el trabajo
choca con la base no cambia nada y el error nombra los archivos, para que la
devuelvas. Una sesión de cliente no ve la pestaña salvo que `api_task_diff`
esté en `dashboard.stakeholder_routes`.

**Sistema › Conocimiento** lista las lecciones del proyecto (`.orch/knowledge/`)
con su área, origen, tarea y fecha, las filtra por área y origen y las busca
igual que la herramienta MCP `orch_knowledge_search`. Por lección muestra
cuántos prompts de despacho la llevaron y, de las tareas terminadas para las
que eran esos prompts, cuántas terminaron al primer intento: una lección que
ningún prompt usó, o cuyas tareas igual necesitaron reintentos, es candidata a
editar o borrar. El operador local agrega, edita y borra lecciones ahí con la
misma función que `orch knowledge add` / `rm`; cada cambio es un evento
`knowledge_edited`, y el commit del archivo lo haces tú. **Curar ahora** corre
`orch knowledge curate` (#452): un diálogo muestra qué haría la curación y
cuánto costaría, y **Confirmar** despacha su única tarea en segundo plano; la
página enlaza entonces esa tarea, que la corrida muestra como cualquier otra. Una curación a la vez. Una sesión de cliente nunca ve la página.

El dashboard está en inglés, español y portugués (Brasil). Arranca en el idioma
de tu navegador y, si no es uno de esos, en inglés; el botón de idioma al pie
de la barra lateral (o ⌘K → *Cambiar idioma*) lo cambia y este navegador
recuerda la elección. `dashboard.language` en `config.yaml` es otro ajuste:
elige lo que lee el **cliente** (página del cliente, resumen ejecutivo, PDF),
no las pantallas del operador. Comandos, flags y claves de config quedan en
inglés.

`--profile operator` (el valor por defecto) muestra todo: tareas, gasto por
modelo, logs. `--profile stakeholder` (el perfil del cliente; el nombre es un
alias de config) protege cada ruta de datos detrás del `--token` y de una
allow-list de rutas seguras para el cliente: sin líneas de log, sin prompts,
sin desglose de costo por modelo salvo que `dashboard.show_spend_to_stakeholder`
esté activo. Mira [`DASHBOARD-PROFILES.md`](DASHBOARD-PROFILES.md) para el
modelo de acceso completo y
[`DELIVERING-TO-STAKEHOLDERS.md`](DELIVERING-TO-STAKEHOLDERS.md) para el
recorrido de cara al cliente (resumen ejecutivo, timeline de fases, ETA,
bloqueos).

---

## 7. Inspeccionar una corrida: status, tasks, events, logs

```bash
# Resumen en una pantalla: tareas por status, gasto, últimos eventos, resumen de la corrida
orch status

# Todas las tareas con status/ruteo/dependencias, filtrable
orch tasks --status todo,in-progress
orch tasks --only 'F1.*'

# Historial de eventos de una tarea
orch events F1.1.T2 --tail 50

# El log crudo del agente para esa tarea
orch logs F1.1.T2 --tail 200
```

Los cuatro aceptan `--json` (salvo `logs`, que tampoco lo tuvo nunca en Python)
para scripts, y comparten los flags de proyecto
(`--project-root`/`--project-id`/`--config`).

Para seguir una corrida en vez de sacar fotos, `orch tui` mantiene esa misma
vista en vivo en la terminal: la tabla de tareas con el tiempo transcurrido de
cada una, el avance por fase, el gasto por proveedor y el log de la tarea
seleccionada como pasos. `↑`/`↓` (o `j`/`k`) eligen una tarea, `f` rota el
filtro de status, `q` sale. Solo lee, así que corre junto a `orch run` en otra
terminal.

```bash
$ orch status --help
The quick check: task counts, costs, last events, run summary (--json for scripts)

Flags:
  -h, --help            help for status
      --json            Emit the raw snapshot as JSON
      --only string     Restrict task rows to ids matching this glob
      --status string   Comma-separated status filter (e.g. todo,in-progress)
```

---

## 8. Arreglar cosas a mano: task set, task-status, reset

```bash
# Cambiar el status de una tarea directamente (las transiciones ilegales salen con exit 3)
orch task-status F1.1.T5 todo --note "re-queued after manual fix"

# Lo mismo, con la superficie más nueva `task set`. No hay flag de milestone:
# un milestone es una fase, y la fase de una tarea vive en tasks.json
orch task set --id F1.1.T5 --status todo

# Rutear una tarea a otro modelo: una clave de model_router.yaml, escrita en tasks.json
orch task set --id F1.1.T5 --model codex/gpt-5.4

# Ver qué tareas in-progress parecen atascadas, sin tocar nada
orch reset

# Devolverlas de verdad a todo
orch reset --requeue --only 'F1.*'
```

```bash
$ orch task set --help
Set a task's status, or its model

Flags:
  -h, --help            help for set
      --id string       Task ID, e.g. F1.1.T3
      --model string    Set the task's model in tasks.json: a model_router.yaml key, which names the backend
      --status string   Set the task status (e.g. done, in-progress, blocked)
```

`reset` lee el status **real** de runtime desde el backend de estado, no el
campo `status` propio de `tasks.json` (que puede estar desactualizado): una
diferencia deliberada respecto de Python, documentada en [`CLI.md`](CLI.md).

---

## 9. El router de modelos

`model_router.yaml` asocia el modelo declarado de una tarea a una CLI de
backend, su nombre `cli_model` y un tier de costo.

**Usar varias IAs.** El `"model"` de una tarea en `tasks.json` es una clave de
`model_router.yaml`, y esa entrada nombra la CLI que la corre. Tres tareas,
tres CLIs:

```json
"tasks": [
  { "id": "T1", "model": "claude/sonnet", ... },
  { "id": "T2", "model": "codex/gpt-5.4", ... },
  { "id": "T3", "model": "gemini/gemini-2.5-flash", ... }
]
```

```yaml
# .orchestrator/model_router.yaml — orch init already writes these entries
claude/sonnet:
  backend: claude
  cli_model: sonnet
  tier: standard
  is_premium: false
codex/gpt-5.4:
  backend: codex
  cli_model: gpt-5.4
  tier: standard
  is_premium: false
gemini/gemini-2.5-flash:
  backend: gemini
  cli_model: gemini-2.5-flash
  tier: cheap
  is_premium: false
```

`orch router validate` comprueba que cada tarea resuelva a una entrada, y
`orch doctor`, que cada CLI que nombran esas entradas esté instalada.

**Esfuerzo de razonamiento.** El `effort:` de una ruta fija cuánto piensa su
CLI. `low`, `medium` y `high` valen en todos los backends (codex, agy y muse
los pasan). Las rutas de codex aceptan además los niveles superiores —
`xhigh`, `max`, `ultra`— que el catálogo de modelos de codex
(`~/.codex/models_cache.json`, que escribe el propio codex) lista para ese
modelo; sin el catálogo, o con un nivel que el modelo no lista, `orch
validate` falla y nombra los niveles que ese modelo acepta. La matriz
completa está en `docs/CONFIG.md`.

```yaml
codex/gpt-6-astra:
  backend: codex
  cli_model: gpt-6-astra
  tier: premium
  effort: xhigh
```

**Failover cuando un proveedor se queda sin cuota.** Por defecto, una tarea con
rate limit espera `retry.rate_limit` y vuelve a intentar con la misma CLI. Dale
a su ruta una lista `failover:` de otras claves de ruta y el reintento va de
inmediato a la primera cuyo proveedor todavía tenga cuota, en el mismo
worktree, así el trabajo hecho se conserva. También cuenta el aviso de claude
de que su ventana se agotó. orch recuerda el proveedor agotado hasta su reset
(la hora que indica claude, o si no el backoff de rate limit), y las tareas
siguientes de esa ruta en la corrida van directo al failover. El gasto, los
eventos y el dashboard muestran el backend que corrió de verdad. Una clave que
no es una ruta hace fallar `orch validate`.

```yaml
claude/sonnet:
  backend: claude
  cli_model: sonnet
  tier: standard
  failover: [codex/gpt-5.4, gemini/gemini-2.5-flash]
```

```bash
# ¿Cada task.model resuelve a una entrada del router?
orch router validate

# Agregar entradas inferidas para lo que no esté ruteado, en un tier dado
orch router add-missing --tier standard
orch router add-missing --yes   # sin la pregunta de confirmación
```

```bash
$ orch router --help
Inspect and maintain model_router.yaml

Available Commands:
  add-missing Append inferred model_router.yaml entries for every unrouted task model
  validate    Check that every task.model resolves to a model_router.yaml entry
```

`add-missing` muestra el plan y pregunta `y/N` antes de escribir, igual que
Python; `--yes` es como un script evita la pregunta (nunca por detección de
TTY: una corrida desatendida con `--mode auto` no debe quedarse pausada en
silencio esperando stdin).

---

## 10. Inspeccionar la config

```bash
orch config show
```

Imprime la config efectiva (los valores por defecto fusionados con
`config.yaml` y cualquier override), seguida de dónde salió cada valor, y las
claves de tu `config.yaml` que orch no reconoce (una buena forma de detectar
una clave mal escrita que en silencio no hace nada). Referencia completa de
claves: [`CONFIG.md`](CONFIG.md).

```bash
$ orch config --help
Config helpers

Available Commands:
  show        Print the effective config, merged with defaults, and where each value came from
```

---

## 11. MCP — que un agente maneje orch directamente

```bash
$ orch mcp --help
Serve orch's state to an MCP-capable agent over stdio.

Seven tools: orch_list_tasks, orch_get_task, orch_set_status,
orch_block, orch_budget, orch_events, orch_context.

Speaks JSON-RPC on stdin/stdout; run it from an MCP client, not
from a terminal. See docs/MCP.md.
```

`orch init` ya escribió un `.mcp.json` que apunta a `orch mcp`: un agente
compatible con MCP (Claude Code incluido) lo toma automáticamente desde la raíz
del proyecto. No hay equivalente en Python; mira [`MCP.md`](MCP.md) para la
lista completa de herramientas y la forma del error de transición ilegal
(nombra cada status al que la tarea puede pasar legalmente, ya que quien llama
es un modelo que puede reintentar).

---

## 12. Instalar los skills de orch

```bash
$ orch install-skills --help
Install orch's skills (the orch manual, the planning pipeline, TDD, E2E) into claude, codex, opencode or cursor

Flags:
      --all              Install every embedded skill (default when --skill is omitted)
      --dry-run          Show what would be installed without writing anything
      --force            Overwrite an already-installed skill of the same name
  -h, --help             help for install-skills
      --path string      Claude target install directory (default: ~/.claude/skills)
      --skill strings    Install only this skill (repeatable); default is --all
      --target strings   Agent(s) to install into: claude, codex, opencode, cursor (repeatable) (default [claude])
```

```bash
orch install-skills                          # en ~/.claude/skills, --all
orch install-skills --target codex --target opencode
orch install-skills --dry-run                # vista previa primero
```

`--target claude` escribe directorios de skills reales bajo `~/.claude/skills`.
`codex`/`opencode` no tienen un mecanismo propio de skills, así que en su lugar
reciben una sección claramente delimitada, que se puede reemplazar de forma
idempotente, al final del `AGENTS.md` del proyecto; `cursor` recibe un archivo
`.cursor/rules/<name>.mdc`. Nueve skills vienen embebidos en el binario:
`orch-tdd`, cómo un agente trabaja una tarea con los tests primero (rojo,
verde, limpiar); `orch-e2e`, cómo un agente recorre una app en marcha en un
navegador y deja specs de Playwright; `orch`, el manual de operación para un
agente que trabaja dentro de un proyecto, y el pipeline de planificación:
`orch-plan` corre `orch-prd` (idea → `docs/prd/`), `orch-arch`
(→ `docs/arch/`), `orch-spec` (→ `specs/`, en el formato exacto que parsea
`orch atomize`) y `orch-tasks` (→ `tasks.json` con
`orch atomize --apply`), y se detiene para ti después de cada documento y antes
de aplicar nada. En un repositorio que ya tiene código, `orch-plan` corre antes
`orch-discover` (→ `docs/arch/000-discovery.md`): qué es el código, cada
afirmación citando su archivo, con los secretos versionados, los efectos
secundarios al arrancar y las preguntas para el cliente, y después propone
`gate.critical_paths`, `guard.protected_paths`, reglas y `verify.commands`, y
aplica solo los que confirmes. La página Sistema › Skills del dashboard tiene
una pestaña **Cómo funciona** que recorre estos pasos (cada skill, qué lee y
qué escribe, el comando que lo inicia) y marca los que ninguna CLI tiene
instalados todavía.

---

## 13. Cuando algo falla

### Corre `orch doctor` primero

```bash
$ orch doctor --help
Environment preflight — provider CLIs, VCS, worktrees, budget config, SQLite

Flags:
  -h, --help   help for doctor
      --json   Emit the full doctor report as JSON on stdout
```

```bash
orch doctor
```

Comprueba: que las CLIs de proveedor referenciadas por
`tasks.json`+`model_router.yaml` estén realmente en el `PATH`, que el ruteo esté
completo, la sensatez del preset de presupuesto, worktrees de git huérfanos, el
estado del VCS (repo/remote/auth), si existe `.mcp.json` y la salud de SQLite.
Misma convención de exit code que `validate`: `0` limpio, `1` solo warnings,
`2` al menos un error.

También es un **informe de preparación**, en cinco secciones — Config (config,
router, tasks.json, modelos), Backends, Tasks, Permissions, Environment — y
termina con un veredicto: ``Ready to `orch run`.`` o lo primero que lo impide.
Tasks dice qué tareas despacharía `orch run` ahora y lista las tareas en
`backlog` cuyas dependencias están terminadas, con `orch task-status <id> todo`
para cada una. Permissions tiene un check `<cli>.permissions` por cada CLI que
usa el proyecto: si su agente puede reportar una tarea terminada (las reglas de
claude y de agy, el permiso `--add-dir` de codex sobre el directorio de
estado). `orch doctor --json` incluye la `section` de cada check y el
`verdict`.

### Tarea bloqueada

1. `orch logs <task-id> --tail 100`: lee la salida del propio agente
2. `orch events <task-id>`: mira lo que registró el propio orch (dispatch,
   fail, retry, budget_pause…)
3. Arregla el spec o el código a mano
4. `orch task-status <task-id> todo --note "..."` para volver a ponerla en cola
5. `orch run` de nuevo: solo toma tareas en `todo`

### `orch` no arranca — exit 1, error de estructura del proyecto

Falta `tasks.json` o `.orchestrator/config.yaml`, o no se puede leer.
`orch init --force` si de verdad debe ser un proyecto real, o revisa las
entradas `config.parse`/`router.parse` de `orch doctor --json` para ver el
error de parseo.

### `orch validate` / `orch doctor` salen con exit 2

Hay al menos un hallazgo con severidad de error: lee la salida legible (o
`--json`) para ver qué check falló y por qué; ambos comandos imprimen todos los
checks que corrieron, no solo los que fallaron.

### Una transición de status ilegal — exit 3

`task-status`/`task set --status` rechazan una transición que la máquina de
estados no permite (p. ej., `done` → `todo` directamente). El error nombra el
status actual de la tarea.

---

## 14. Actualizar orch

```bash
# Vuelve a correr el instalador: siempre trae el último release v* (sin -py)
curl -fsSL https://raw.githubusercontent.com/Guria-Studio/orch/main/scripts/install.sh | sh

# o: orch upgrade

# Verifica
orch --version
```

Actualizar el binario nunca toca el `.orchestrator/config.yaml`,
`model_router.yaml` ni `budgets.yaml` propios de un proyecto. Si quieres llevar
un valor por defecto nuevo a un proyecto existente, corre `orch init --force`
ahí y revisa el diff antes de hacer commit (sobrescribe los ajustes propios del
proyecto, así que no lo hagas a ciegas).

---

## 15. Funciones por capa

Cada comando de orch, agrupado por quién lo necesita y para qué. El núcleo es
el camino que recorre este manual; las otras capas están para cuando las
necesites, y ninguna hace falta para empezar. [`CLI.md`](CLI.md) tiene todos
los flags.

**Núcleo — agentes como equipo, una página para tu cliente**

| Comando | Para qué sirve |
|---|---|
| `orch start "idea"` | El primer comando: configura el proyecto, planifica la idea con claude, la estima y corre las primeras tareas ([§2](#2-crear-un-proyecto-nuevo)). |
| `orch run` | Despacha cada tarea lista a su agente en paralelo, cada una en su worktree y su PR, revisada antes del push y con el CI vigilado ([§5](#5-run--despachar-tareas-a-los-agentes-de-ia)). |
| `orch dashboard` | Tu pantalla: los agentes trabajando, lo que te necesita, el diff de cada tarea para aceptarlo o devolverlo, y el botón Share para un enlace en vivo para el cliente ([§6](#6-abrir-el-dashboard)). |
| `orch publish` | Escribe la página del cliente como sitio estático, en una carpeta o una rama `gh-pages` (`--watch` la mantiene al día). |
| La página del cliente | Lo que abre tu cliente: avance por fase, bloqueos en palabras simples, la fecha estimada de entrega. En vivo detrás de un token, o publicada. |

**Agencias — varios clientes a la vez**

| Comando | Para qué sirve |
|---|---|
| `orch dashboard --portfolio "~/clients/*"` | Todos los proyectos de tus clientes en un solo dashboard, para ver cuál te necesita sin cambiar de carpeta. |
| `orch cloud setup` + `orch publish --to cloud` | Aloja la página de cada cliente en un Cloudflare Worker de tu propia cuenta, con un enlace estable que funciona con tu máquina apagada ([`CLOUD.md`](CLOUD.md)). |
| `orch report pdf` | La página del cliente como un PDF A4 de una página, para adjuntar a un correo o una factura. |
| `orch notify digest` | El resumen ejecutivo y el avance de cada fase, impreso o enviado a Slack/Discord (`--send`); ponlo en cron para un informe semanal. |
| `presentation.branding` | El nombre, el logo y el color de tu agencia en la página del cliente y el PDF en lugar de los de orch ([`CONFIG.md`](CONFIG.md)). |

**Integraciones — las herramientas que tu cliente ya usa**

| Comando | Para qué sirve |
|---|---|
| `orch sync issues` | Convierte los issues de GitHub etiquetados del cliente en tareas (`gh-<número>`), y en cada corrida agrega solo lo nuevo. |
| `orch atomize --from-issues label:orch --model M` | Convierte cada issue abierto de GitHub que coincide con la búsqueda en un spec (`specs/issue-<n>-<slug>.md`) y sus tareas; muestra la vista previa y con `--apply` escribe. Una segunda corrida no importa nada dos veces. El PR de la tarea cierra el issue al mergear (`Closes`); con `vcs.provider: local` orch lo cierra con un comentario cuando aterriza su última tarea. |
| `orch atomize --from spec-kit --model M` (o `--from openspec`) | Convierte el `specs/<feature>/tasks.md` de cada feature de spec-kit (o el `openspec/changes/<change>/tasks.md` de cada change de OpenSpec) en un spec, `specs/speckit-<feature>.md` / `specs/openspec-<change>.md`, y sus tareas: las fases, los grupos `[P]` y los `depends on` pasan a dependencias, y las tareas marcadas quedan fuera. Muestra la vista previa y con `--apply` escribe; una segunda corrida con el `tasks.md` igual no cambia nada ([`SPEC-IMPORT.md`](SPEC-IMPORT.md)). |
| `orch export clickup` | Refleja el plan en una Folder de ClickUp para un cliente que sigue el trabajo ahí; al volver a correrlo, los estados se actualizan. |
| `orch export multica` | Crea el plan como issues de Multica, un padre por fase. |
| `orch ci setup` / `ci review` / `ci usage` | Escribe el pipeline de CI que pide el repositorio, corre una revisión con IA de cada PR dentro del CI y suma los minutos de Actions facturados. |
| `orch mcp` | Deja que un agente compatible con MCP lea y mueva las tareas de orch como herramientas ([§11](#11-mcp--que-un-agente-maneje-orch-directamente)). |

**Avanzado — ajuste y reparación**

| Comando | Para qué sirve |
|---|---|
| `orch bench` | Corre el mismo proyecto con cada proveedor y compara tareas terminadas, tiempo y gasto (agentes reales, con tope en `--max-usd`). |
| `orch router suggest` / `router refresh` | Qué ruta funcionó para qué tipo de tarea, con sugerencias más baratas o más fuertes; rutas para los modelos que ganaron tus CLIs ([§9](#9-el-router-de-modelos)). |
| `orch knowledge curate` / `knowledge stats` | Une y poda las lecciones que escribieron los agentes, como un PR; mide el éxito al primer intento antes y desde la primera lección. |
| `orch provider disable` / `enable` | Pausa un proveedor mientras falla; sus tareas esperan en vez de fallar. |
| `orch budget set` | Cambia el presupuesto de tokens, la ventana, el umbral o la fuente (`tokens` / `provider`) de un proveedor sin editar YAML. |
| `orch budget show` | La ventana de cada proveedor, qué la decide (los tokens de este proyecto o la cuota reportada del plan) y cuándo se reinicia. |
| `orch task salvage` | Acepta el trabajo que un intento anterior commiteó pero nunca publicó, sin volver a correr el agente. |
| `orch reset` | Encuentra tareas atascadas en progreso tras una caída, y las vuelve a encolar con `--requeue` ([§8](#8-arreglar-cosas-a-mano-task-set-task-status-reset)). |
| `orch dashboard token` | Rota o inspecciona el token que necesita un enlace de cliente. |
| `orch config show` / `set` / `unset` | La config efectiva y de dónde sale cada valor; edita claves conservando los comentarios del archivo ([§10](#10-inspeccionar-la-config)). |
| `orch findings` | La cola de reportes de bugs que los agentes escribieron sobre el propio orch; lee cada uno y envíalo o descártalo. |
| `orch upgrade` | Instala la última versión sobre este binario, verificada con checksum ([§14](#14-actualizar-orch)). |
| `orch claude-permissions` / `agy-permissions` | Agrega las reglas que esas CLIs necesitan para que sus agentes puedan reportar una tarea como terminada. |

---

## Python legacy

El `orch` anterior a la reescritura (el dashboard en FastAPI,
`pipx install orch`, toda la historia de sprints que este binario en Go está
reemplazando funcionalidad por funcionalidad) está congelado en la rama
`python-legacy`, con el tag **`v0.11.0-py`**. Todavía se instala y corre
exactamente como lo documenta el manual propio de ese tag:

```bash
git checkout v0.11.0-py
pipx install .
```

o

```bash
pipx install git+https://github.com/Guria-Studio/orch.git@v0.11.0-py
```

Esa línea no recibe funcionalidades nuevas; solo avanza el binario en Go. Mira
[`RELEASING.md`](RELEASING.md) para saber por qué las dos líneas comparten un
mismo espacio de tags, separadas por un sufijo `-py` en vez de un prefijo de
rama aparte.

---

## Referencias

- Referencia completa de flags de la CLI, notas de paridad con Python: [`CLI.md`](CLI.md)
- Claves de config: [`CONFIG.md`](CONFIG.md)
- Formato markdown de los specs: [`SPEC-FORMAT.md`](SPEC-FORMAT.md)
- Servidor y herramientas MCP: [`MCP.md`](MCP.md)
- Modelo de acceso del dashboard: [`DASHBOARD-PROFILES.md`](DASHBOARD-PROFILES.md)
- Compartir la página del cliente: [`DELIVERING-TO-STAKEHOLDERS.md`](DELIVERING-TO-STAKEHOLDERS.md)
- Proceso de release y esquema de tags: [`RELEASING.md`](RELEASING.md)

## Feedback

Este manual es un documento vivo, que se actualiza en el mismo PR que agrega un
subcomando nuevo. Si te encuentras con un caso que no está cubierto, abre un
issue en <https://github.com/Guria-Studio/orch/issues> o manda un PR con la
sección que falta.
