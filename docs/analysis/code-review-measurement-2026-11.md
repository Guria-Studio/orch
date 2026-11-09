# Medición del code review de Gemini en CI: 50 PRs (noviembre 2026)

Issue: #521 (paraguas #518). Medido el 09/11/2026 sobre `main` @ `f4604be`.

## Resumen

- **Conclusión: conviene mantenerlo, pero endurecido.** Apagarlo no se justifica. Así como está, cuesta más atención de la que devuelve.
- En los 50 PRs dejó **97 hallazgos únicos**, 36 bloqueantes y 61 menores. **42 (43 %) no afirman nada sobre el código**: dicen "no se puede verificar desde el diff". Casi siempre pasa porque el diff llegó truncado.
- **Precisión literal: 86 %.** De los 70 hallazgos que se pudieron decidir, 60 son ciertos según el diff o el checklist.
- **Precisión útil: 25 % en total y 53 % en los bloqueantes.** Útil quiere decir que señala un defecto o un hueco que valía la pena arreglar. Los bloqueantes útiles fueron 19 de 36.
- **El veredicto "Changes requested" estuvo justificado en 7 de 11 PRs (64 %).** Esos 7 tenían al menos un bloqueante útil.
- **5 PRs se mergearon con `gemini-review` en rojo.** En 3 la decisión fue correcta, porque los bloqueantes eran ruido. En los otros 2 se ignoraron hallazgos reales: #564 tiene una rama sin test y #488 activa el guard por defecto en proyectos existentes, que es la regla 2.
- **Recall estimado: entre 55 % y 75 % para bugs de comportamiento**, con un método débil (ver "Límites"). Dentro de la ventana hay 2 bugs perdidos confirmados y 3 dudosos. La auditoría del 07/11 encontró bugs más viejos en código que Gemini había aprobado.
- **Hallazgos estructurales**, más importantes que la precisión:
  1. Los **10 PRs que la política marcó como auto-mergeables no recibieron review**: el job `policy` falla al armar el auto-merge y `gemini-review` depende de él. Son justo los PRs que se suponen seguros para entrar solos.
  2. El revisor **nunca ve la spec**. Recibe el título y el cuerpo del PR, la lista de archivos y el diff. El issue que el PR cierra no entra, y 36 de los 50 PRs cierran un issue.
  3. Al menos **7 de 36 PRs** con log disponible se revisaron con el **diff truncado**: el revisor vio entre el 36 % y el 90 % del diff.
  4. En un **re-run**, el título y el cuerpo vienen del payload del evento original. Si el PR se editó después, el revisor lee la versión vieja, y eso produjo un falso positivo en #517.
- **Costo y latencia**: el paso del revisor tarda 144 s de mediana y 285 s en el p90. Hubo 1,6 corridas por PR. El costo en dinero no aparece en los logs. El review solo falló 2 veces (#575 en un commit intermedio y #509).

## Método

### Muestra

- Son los 50 PRs mergeados más recientes, desde #478 (24/10) hasta #597 (09/11), que tienen el comentario `<!-- orch:gemini-review -->`.
- En ese rango se mergearon 61 PRs. Los otros 11 no tienen review: 10 por el problema estructural 1 y #509 porque el revisor falló con el diff gigante del cambio de repo.

### Fuentes

| Qué | De dónde |
|---|---|
| Datos de cada PR | `gh pr view N --json comments,reviews,commits,files,body,closingIssuesReferences` |
| Veredictos anteriores (el comentario se edita en el lugar) | El historial de ediciones del comentario por GraphQL (`userContentEdits`) |
| Veredicto por commit | Los check runs `gemini-review` de cada commit (título y conclusión) |
| Tiempos | `actions/runs/<id>/jobs`, en 79 corridas |
| Tamaño del prompt y truncado | La línea `prompt is N bytes (diff D, budget B)` del log, disponible en 36 PRs |
| Qué recibe el revisor | `.github/workflows/review.yml`, función `build_head` |

En 3 PRs (#571, #574 y #575) el historial de ediciones está incompleto. Para esos usé los check runs.

### Unidad y clasificación

Un hallazgo es un ítem bloqueante o menor del comentario. Si un mismo hallazgo se repite en varios re-reviews del mismo PR, se cuenta una vez. Cada uno se clasificó en tres ejes:

- **Correcto / falso / sin decidir.** Lo verifiqué leyendo el código mergeado (`git show <merge>:<archivo>`), los tests y el cuerpo del PR. "Sin decidir" son los NV que no comprobé, casi todos por truncado.
- **Útil o no.** Útil significa que señala un defecto real o un hueco que vale la pena cerrar. Un hallazgo literalmente correcto pero sin valor es "no útil", por ejemplo `defer func(){ _ = f.Close() }()` sobre un archivo de solo lectura, o rutas de ejemplo bajo `/home/` (placeholders).
- **Atendido o no.** Atendido significa que un commit posterior del mismo PR, o el cuerpo actualizado, lo resolvió.

Hay dos precisiones. La **literal** es correctos / (correctos + falsos). La **útil** es útiles / total. La brief de #521 define verdadero positivo como "atendido o verificablemente correcto", que corresponde a la precisión literal. La útil es la que importa para decidir.

### Falsos negativos

Busqué bugs posteriores en el código que tocaron estos PRs, dentro de los 14 días siguientes, de cuatro maneras:

1. Commits `fix` en `main` que tocan los mismos archivos fuente.
2. Commits posteriores cuyo mensaje nombra el PR o su issue.
3. Issues `type:bug` o `auto-reported` creados desde el 24/10.
4. Los issues de la auditoría del 07/11 (#527–#549), rastreando con `git log -S` el commit que introdujo cada línea citada.

Cada candidato lo leí a mano. Una coincidencia de archivo sola no cuenta: `reaper.go` lo tocan casi todos los PRs.

## Resultados

### Totales

| | Hallazgos | Correctos | Falsos | Sin decidir | Útiles | Atendidos |
|---|---:|---:|---:|---:|---:|---:|
| Todos | 97 | 60 | 10 | 27 | 24 (25 %) | 27 (28 %) |
| Bloqueantes | 36 | 33 | 3 | 0 | 19 (53 %) | 20 (56 %) |
| Menores concretos | 19 | 14 | 2 | 3 | 2 (11 %) | 3 |
| Menores NV ("no se puede verificar") | 42 | 13 | 5 | 24 | 3 (7 %) | 4 |

- **Veredictos finales**: 25 Approve, 20 Comments only y 5 Request changes, estos últimos mergeados igual.
- **Primer veredicto "Request changes"**: 11 PRs. En 6 se resolvió con commits (#591, #563, #496, #493, #491 y #479). En 5 se mergeó en rojo (#564, #560, #510, #489 y #488).
- **Precisión del bloqueo por PR**: 7 de 11. Los 7 justificados son #564, #563, #496, #493, #491, #488 y #479. Los 4 que bloquearon sin un hallazgo útil son #591, #560, #510 y #489.
- **De dónde salen los 17 bloqueantes no útiles**:
  - 11 vienen de la regla 19 aplicada al pie de la letra: `defer _ = closeDB()` (idioma que aparece 32 veces en el repo), errores de `os.*` que ya traen la ruta, `%v` en lugar de `%w`, y un `ParseInt` que en realidad sí se maneja.
  - 3 son "secretos" que no lo son: el repo público `billing-api` y la contraseña falsa del test del propio escáner de secretos.
  - 2 son rutas de ejemplo bajo `/home/` (placeholders).
  - El restante es la rama del login vacío en #564.

### Tabla por PR

Columnas:
- **Veredictos**: A = Approve, C = Comments only, RC = Request changes, en orden.
- **Hallazgos**: bloqueantes / menores, con los NV entre paréntesis.
- **Spec**: ningún review la recibió. 36 de los 50 PRs cierran un issue cuyo cuerpo el revisor no vio.

| PR | Líneas | Veredictos (en orden) | Hallazgos B/m (NV) | Correctos / falsos / ? | Útiles | Atendidos | Bugs perdidos (≤14 d) |
|---|---:|---|---|---|---:|---:|---|
| #597 | 2090 | C | 0/6 (6) | 0 / 0 / 6 | 0 | 0 | — (ventana < 2 días) |
| #596 | 1333 | C | 0/3 (3) | 1 / 2 / 0 | 0 | 0 | — (ventana < 2 días) |
| #595 | 1549 | C→A→C | 0/5 (2) | 4 / 0 / 1 | 2 | 2 | — (ventana < 2 días) |
| #594 | 468 | A | 0 | — | — | — | — (ventana < 2 días) |
| #593 | 434 | A | 0 | — | — | — | — (ventana < 2 días) |
| #592 | 271 | A | 0 | — | — | — | — (ventana < 2 días) |
| #591 | 637 | RC→A | 1/0 (0) | 1 / 0 / 0 | 0 | 1 | — (ventana < 2 días) |
| #590 | 457 | A | 0 | — | — | — | — (ventana < 2 días) |
| #589 | 309 | A | 0 | — | — | — | — (ventana < 2 días) |
| #586 | 1517 | C | 0/2 (2) | 1 / 0 / 1 | 1 | 1 | — (ventana < 2 días) |
| #585 | 362 | A | 0 | — | — | — | — (ventana < 2 días) |
| #584 | 363 | A | 0 | — | — | — | — (ventana < 2 días) |
| #583 | 552 | C | 0/1 (1) | 1 / 0 / 0 | 0 | 0 | — (ventana < 2 días) |
| #575 | 263 | A | 0 | — | — | — | — (ventana < 2 días) |
| #574 | 422 | C | 0/2 (2) | 2 / 0 / 0 | 0 | 0 | — (ventana < 2 días) |
| #572 | 552 | C | 0/3 (3) | 0 / 0 / 3 | 0 | 0 | — (ventana < 2 días) |
| #571 | 356 | C | 0/1 (1) | 0 / 0 / 1 | 0 | 0 | — (ventana < 2 días) |
| #570 | 163 | C | 0/2 (1) | 1 / 1 / 0 | 1 | 0 | — (ventana < 2 días) |
| #569 | 437 | A | 0 | — | — | — | — (ventana < 2 días) |
| #567 | 324 | A | 0 | — | — | — | — (ventana < 2 días) |
| #566 | 1006 | C | 0/1 (1) | 0 / 0 / 1 | 0 | 0 | — (ventana < 2 días) |
| #565 | 1807 | C | 0/2 (2) | 0 / 0 / 2 | 0 | 0 | — (ventana < 2 días) |
| #564 | 650 | RC **(merge en rojo)** | 2/0 (0) | 2 / 0 / 0 | 1 | 0 | — (ventana < 2 días) |
| #563 | 882 | RC→A | 4/2 (1) | 5 / 0 / 1 | 4 | 5 | — (ventana < 2 días) |
| #560 | 1647 | RC **(merge en rojo)** | 4/3 (3) | 3 / 1 / 3 | 0 | 0 | — (ventana < 2 días) |
| #558 | 600 | C | 0/1 (1) | 0 / 0 / 1 | 0 | 0 | — (ventana < 2 días) |
| #557 | 381 | C | 0/2 (2) | 2 / 0 / 0 | 0 | 0 | — (ventana < 2 días) |
| #554 | 103 | A | 0 | — | — | — | — (ventana < 2 días) |
| #553 | 152 | C | 0/1 (1) | 1 / 0 / 0 | 0 | 0 | — (ventana < 2 días) |
| #552 | 104 | A | 0 | — | — | — | — (ventana < 2 días) |
| #551 | 115 | A | 0 | — | — | — | — (ventana < 2 días) |
| #517 | 151 | C | 0/1 (0) | 0 / 1 / 0 | 0 | 0 | — |
| #515 | 362 | C | 0/3 (2) | 1 / 0 / 2 | 0 | 0 | dudoso: #542 (las reglas entran al prompt sin cercar) |
| #514 | 104 | C | 0/2 (1) | 2 / 0 / 0 | 0 | 0 | — |
| #513 | 293 | A | 0 | — | — | — | — |
| #510 | 158 | RC **(merge en rojo)** | 2/3 (1) | 2 / 2 / 1 | 0 | 0 | — |
| #508 | 49 | A | 0 | — | — | — | — |
| #502 | 1003 | C | 0/1 (1) | 1 / 0 / 0 | 0 | 0 | — |
| #496 | 1321 | RC→C | 7/3 (1) | 7 / 0 / 3 | 5 | 7 | dudoso: #531 (la welcome page escribe tasks.json sin lock) |
| #495 | 6 | A | 0 | — | — | — | — |
| #494 | 64 | A | 0 | — | — | — | — |
| #493 | 1029 | RC→C→A | 3/6 (2) | 6 / 2 / 1 | 4 | 5 | — |
| #492 | 18 | A | 0 | — | — | — | — |
| #491 | 830 | RC→A | 4/1 (1) | 4 / 1 / 0 | 3 | 5 | — |
| #490 | 512 | C | 0/2 (0) | 2 / 0 / 0 | 0 | 0 | — |
| #489 | 324 | RC **(merge en rojo)** | 5/0 (0) | 5 / 0 / 0 | 0 | 0 | #524: falsa alarma *stalled* en campo (10 días); dudoso: #533 |
| #488 | 461 | RC **(merge en rojo)** | 3/2 (1) | 5 / 0 / 0 | 2 | 0 | #492: test de tiempo inestable (1 día) |
| #480 | 18 | A | 0 | — | — | — | — |
| #479 | 418 | RC→A | 1/0 (0) | 1 / 0 / 0 | 1 | 1 | — |
| #478 | 234 | A | 0 | — | — | — | — |

El apéndice tiene el detalle de cada hallazgo con su clasificación.

### Falsos negativos y recall

**Confirmados en la muestra:**

- **#488 → #492.** `TestScanIsFastOnALargeDiff` medía tiempo de reloj contra un presupuesto de 500 ms, y bajo `-race` en una máquina cargada llegó a 575 ms. Se arregló al día siguiente; el cuerpo de #492 dice "from #488". El checklist no tiene regla sobre asserts de tiempo de reloj.
- **#489 → #524.** La heurística de *stalled* era "el log calla N minutos". En campo marcó como trabada una tarea de tradex que llevaba 1 h descargando datos (163 MB escritos). El issue #487 pedía "sin falsas alarmas en una corrida sana". Con la spec a la vista, la heurística era discutible. Sin ella, el revisor solo vio que los errores no estaban envueltos.

**Dudosos.** El PR contribuyó, pero el defecto ya existía o es de diseño:

- **#489 → #533.** *stalled* solo se pinta en la línea de tiempo.
- **#496 → #531.** La welcome page suma otro escritor de `tasks.json` vía `atomize.Apply`, sin lock.
- **#515 → #542.** Las reglas del operador entran al prompt sin cercar como datos.

**Fuera de la muestra, como contexto.** La auditoría del 07/11 abrió 13 bugs en `main` (#527–#549). Tres los rastreé hasta PRs que Gemini había aprobado:

| Bug | Origen | Veredicto de Gemini en el origen |
|---|---|---|
| #532: `openPRFor` se traga el error | #410 | Approve |
| #549: `/api/usage` sin llamador | #423 | Approve |
| #549: Hold sin forma de desbloquear | #372 | Approve |

Son bugs semánticos de flujo: el diff solo no alcanza para verlos.

**Estimación del recall.**

- Gemini atrapó 6 defectos de comportamiento:
  - exec de claude sin process group ni timeout (#496)
  - `run.Wait` descartado (#496)
  - errores de `ReadActivity` y `term.Restore` tragados (#491)
  - `RowsAffected` descartado (#563)
  - guard activado por defecto (#488, ignorado)
- Dentro de la ventana se le escaparon entre 2 (confirmados) y 5 (sumando los dudosos).
- Eso da un **recall de 55 % a 75 %**, contando solo bugs de comportamiento.
- Si se suman los huecos de test que señaló y que se cerraron, sube, pero mezclar las dos cosas sería inflarlo.

### Ejemplos

**Verdaderos positivos que valieron la pena:**

- **#496.** "The command running claude does not configure `SysProcAttr.Setpgid = true` and runs under a context without a timeout" (regla 18). Era cierto: un planner colgado habría dejado huérfanos. Se arregló en `f29169f`. En el mismo PR, en revisiones sucesivas, encontró también `time.Sleep` en un test, `run.Wait()` descartado y la rama `Run` sin test. Los 7 bloqueantes se atendieron.
- **#563.** "The error return from `res.RowsAffected()` is silently dropped" y las ramas `Stopped`/`TimedOut`/aprobado de `attemptRow` sin test. Ambos se atendieron (`6a106a0`, `d560b63`). Es justo la tabla que hoy usa `orch metrics`.
- **#488.** "Enabling guard.secrets as block … by default changes behavior for existing projects" (regla 2), más que los patrones de AWS, Slack, OpenAI, Google y Stripe no tenían test (verificado: solo se probaban GitHub, clave privada y debug). Se mergeó en rojo sin atenderlos.
- **#595.** Dos hallazgos NV ("no puedo ver si está probado") resultaron ser huecos reales, y el autor agregó los tests en `9019ce7`. O sea que un NV a veces sirve, pero no de forma confiable: 3 útiles de 42.

**Falsos positivos:**

- **#510.** Bloqueó por "private repository URL (`billing-api`)" como secreto (regla 5), pero ese repo es **público** y es el repo demo que enlazan los docs.
- **#560.** Bloqueó por 4 hallazgos de la regla 19: tres `defer func() { _ = closeDB() }()`, un idioma que el repo usa 32 veces, y un `strconv.ParseInt` que "se descarta" cuando en realidad devuelve `withExitCode(2, …)`.
- **#489.** Bloqueó con 5 hallazgos de la regla 19 en `readTail`: `os.Open`/`Stat`/`ReadAt` sin envolver (el `*PathError` ya trae la ruta) y un `defer f.Close()` de solo lectura. Ninguno es un defecto. Se mergeó en rojo.
- **#517.** "Conventional commit" sobre un título que ya se había corregido. El re-run leyó el título del payload original.
- **#597.** Seis menores del tipo "cannot check … because it is truncated from the diff". El prompt cortó el diff al 40 %.

### Contexto que recibe el revisor

Según `review.yml` (`build_head`), el prompt tiene, en este orden:

1. Instrucciones y el esquema JSON.
2. `CHECKLIST.md` (12,9 KB).
3. Título y cuerpo del PR.
4. La lista de archivos.
5. El diff, cortado para que el prompt total no pase de 120 000 bytes.

Faltan tres cosas:

- **La spec.** El issue que cierra el PR no entra. Solo llega lo que el autor copie al cuerpo. Desde #574, el review local de `verify` sí recibe la tarea y su spec. El de CI no.
- **El código alrededor del diff.** El revisor no tiene herramientas (`"tools": {"core": []}`), así que no puede abrir un test que no esté en el diff. De ahí sale la mayoría de los NV.
- **Un diff completo en PRs grandes.** El tope de 120 KB existe porque el prompt viaja como una sola variable de entorno (`MAX_ARG_STRLEN`, ver el comentario en `review.yml`), no por la ventana del modelo.

### Costo y latencia

- **Latencia.** En 79 corridas, el paso "Run the reviewer" tardó 144 s de mediana, 285 s en el p90 y 426 s como máximo. El job completo tardó 157 s de mediana. Corre en paralelo con `go-test`, así que en la práctica casi no alarga el CI.
- **Corridas.** Fueron 1,6 por PR. No hubo ningún reintento por respuesta vacía. El review falló 2 veces: #575 en un commit intermedio y #509 con el diff del cambio de repo.
- **Modelo.** `GEMINI_REVIEW_MODEL` no está definida, así que usa el default del CLI. Según `docs/CI-REVIEW.md`, el default observado es `gemini-3.1-flash-lite`.
- **Costo en dinero.** **No es visible.** El action no deja tokens en el log. Como cota, cada corrida manda como máximo unos 120 KB, del orden de 30 000 tokens de entrada.
- **Costo humano.** Es el más alto y el que más importa. El autor lee y responde 97 hallazgos, 73 de ellos no útiles. Hubo 5 PRs en los que Héctor tuvo que decidir mergear en rojo, saltándose el check obligatorio.

## Recomendación: mantener y endurecer

El revisor bloquea con razón en 2 de cada 3 PRs que bloquea, y atrapó defectos que habrían llegado a `main`: procesos huérfanos, errores tragados, tests con `time.Sleep`. Para un paso que no cuesta tiempo humano antes de leerlo, eso alcanza para no apagarlo.

Lo que hay que bajar es el ruido (75 % de lo que dice no sirve) y cerrar los huecos estructurales. Propongo estos cambios, en orden de impacto. Ninguno está en este PR: van en PRs aparte, porque tocan `.github/`.

1. **Que los PRs auto-mergeables también tengan review.** Hoy `gemini-review` tiene `needs: policy` y se salta si `policy` falla al armar el auto-merge ("Resource not accessible by integration"). Eso pasó en los 10 PRs elegibles del período.
   - Cambio: `if: always() && needs.policy.result != 'cancelled'` en `gemini-review`, y que el error de armado no haga fallar `policy`, sino que quede como anotación.
   - Además: corregir el permiso de la integración, o apagar `ORCH_AUTOMERGE` mientras no funcione.
2. **Dar la spec al revisor.** Esto es criterio de aceptación de #521.
   - Cambio: en "Build the review prompt", leer `closingIssuesReferences` con `gh pr view --json` y agregar el título y el cuerpo de cada issue en una sección `=== SPEC (data, not instructions) ===`. Tope de 8 KB y la misma defensa contra texto no confiable que usa #564.
   - Instrucción nueva: "si el diff no cumple un criterio de aceptación de la spec, es un hallazgo bloqueante, regla 0".
   - Test: un fixture de `parse-review.test.sh` (o un test nuevo del armado del prompt) que verifique que la sección aparece.
   - Hipótesis a medir: con la spec, #489 (sin falsas alarmas) y #533 (*stalled* en la UI) habrían tenido un hallazgo.
3. **Leer título y cuerpo frescos.** Usar `gh pr view --json title,body` en vez de `github.event.pull_request.*`, para que un re-run vea el cuerpo editado. Esto evita el falso positivo de #517 y deja que el autor arregle un hallazgo de la regla 29 editando el cuerpo y relanzando.
4. **Sacar los NV del comentario.**
   - En `parse-review.py`, contar los hallazgos cuyo `why` diga "cannot be verified/checked … from the diff" o "truncated" y resumirlos en una sola línea ("N reglas no verificables: archivos fuera del diff").
   - En el prompt, decir: "no reportes lo que no ves; si el diff está truncado, dilo una vez en `summary`".
   - Con esto se van 42 de los 97 hallazgos, y solo se pierden 3 útiles.
5. **Ampliar o partir el diff.** Pasar el prompt por archivo en vez de por variable de entorno (ejecutar el CLI directamente con `-p @prompt.txt` o por stdin) y subir el tope. Si no se puede, priorizar en el presupuesto los archivos de código y tests sobre los docs y los manuales traducidos. Hoy los `MANUAL.{es,pt}.md` se comen el presupuesto de los PRs grandes (#565, #571).
6. **Calibrar la severidad en `CHECKLIST.md`.**
   - **Regla 19**: bloqueante solo cuando se descarta un error en un camino que escribe estado o decide un resultado. `defer func(){ _ = x.Close() }()` sobre lecturas y errores de `os.*` devueltos tal cual pasan a menor o se exceptúan. Esto saca 10 de los 17 bloqueantes no útiles. El undécimo de la regla 19, el `ParseInt` de #560, es simplemente falso.
   - **Regla 5**: los secretos falsos en tests de un escáner o redactor de secretos (`internal/guard`, `internal/findings`) son una excepción declarada. Una URL a un repo **público** no es un secreto. Saca 3.
   - **Regla 6**: las rutas de ejemplo bajo `/home/` que son placeholders evidentes son menores. Saca 2.
   - **Regla 22 ampliada**: prohibir también los asserts de tiempo de reloj en tests (`elapsed < X`) sin un margen que contemple `-race` y una máquina cargada. Habría atrapado #488→#492.
   - **Regla 2**: se queda como está. Fue la que más valor dio en #488.
7. **Umbral documentado de cuándo el veredicto bloquea el merge.** Es el tercer criterio de #521. Propuesta para `docs/CI-REVIEW.md`:
   - `gemini-review` en rojo solo si hay un bloqueante de las reglas 0 (spec), 2, 3, 4, 5, 16, 17, 18, 19 (camino de escritura), 22, u 8 cuando la rama sin test cambia estado o salida. Todo lo demás es menor y no pone el check en rojo.
   - Mergear en rojo exige un comentario en el PR que diga por qué cada bloqueante no aplica. Es lo que ya se hace al responder hallazgos (#491, #493, #596). Ninguno de los 5 PRs mergeados en rojo tiene ese comentario.
   - Regla de apagado: medir de nuevo en 30 PRs con los cambios 1 a 6 aplicados. Si menos del 50 % de los bloqueantes es útil, o si la mitad de los "Request changes" se mergea en rojo, **pasar a solo-comentario**: sacar `gemini-review` de los checks obligatorios. Si además los hallazgos útiles por PR bajan de 0,2, apagarlo (`gh workflow disable "PR review"`).
8. **Guardar los datos para la próxima medición.** Subir `review.json` como artefacto con retención de 90 días (hoy es de 7 y solo guarda el texto crudo), o agregarlo a un JSONL. Esta medición dependió del historial de ediciones de un comentario, y ese historial está incompleto en algunos PRs.

## Límites del método

- **La precisión "útil" es mi criterio.** Lo apliqué leyendo el código, pero otro lector podría mover varios casos de borde, sobre todo los de la regla 29 y los tests de ramas menores. El apéndice lista cada decisión para que se pueda auditar.
- **El recall está inflado.** 31 de los 50 PRs se mergearon el 08 y el 09/11, así que su ventana de observación es de menos de 2 días. Los demás tienen entre 5 y 16 días. Un bug que todavía nadie encontró no cuenta.
- **El recall también depende del método de búsqueda.** Solo cuenta lo que dejó rastro en un issue o en un commit `fix` que se pudo atribuir. Los bugs que el autor arregló antes de abrir el PR no son visibles.
- **27 hallazgos quedaron sin decidir.** Son casi todos NV por truncado, y ninguno cambia las conclusiones. Si todos fueran útiles, la precisión útil subiría al 53 %, y es muy improbable que lo sean.
- **La auditoría del 07/11 tiene un sesgo propio.** Encontró bugs de muchas semanas, y aquí solo uso los que caen en la muestra. Los otros tres se citan como contexto.
- **Los tiempos y el truncado** salen de los logs disponibles: 79 corridas para los tiempos y 36 PRs para el truncado. Los logs más viejos ya no tienen la línea del tamaño del prompt.

## Apéndice: cada hallazgo

Notación: bloqueante o menor; NV = "no se puede verificar desde el diff". Luego la clasificación correcto/falso, útil o no y, si corresponde, atendido.

- **#597**
  - menor, NV — 6 NV por truncado (test budgetshow, migración 016, imports, errores, fixtures) → sin decidir, no útil
  - menor, NV — (ídem) → sin decidir, no útil
  - menor, NV — (ídem) → sin decidir, no útil
  - menor, NV — (ídem) → sin decidir, no útil
  - menor, NV — (ídem) → sin decidir, no útil
  - menor, NV — (ídem) → sin decidir, no útil
- **#596**
  - menor, NV — App.tsx sin test de render: cierto, aceptado (no hay harness) → correcto, no útil
  - menor, NV — liveness_other.go: el parseo sí tiene test → falso, no útil
  - menor, NV — regla 30 en translate.go: ya hidrata → falso, no útil
- **#595**
  - menor, NV — warning de source_issue sin test -> test agregado 9019ce7 → correcto, útil, atendido
  - menor, NV — --model sin --from-issues sin test -> test agregado 9019ce7 → correcto, útil, atendido
  - menor — ramas de verifyNote sin test (cierto, bajo valor) → correcto, no útil
  - menor — planIssueImport: id ya tomado sin test (no concluyente) → sin decidir, no útil
  - menor — CloseIssue con %s+mergeDetail: convención del paquete → correcto, no útil
- **#591**
  - bloqueante — ruta de ejemplo bajo /home en CONFIG.md: placeholder, se cambió igual → correcto, no útil, atendido
- **#586**
  - menor, NV — helpers de worktree movidos sin test -> 7973c8f → correcto, útil, atendido
  - menor, NV — verify_test truncado → sin decidir, no útil
- **#583**
  - menor, NV — rama runner nil en AskOnce sin test → correcto, no útil
- **#574**
  - menor, NV — SpecSection sin test directo → correcto, no útil
  - menor, NV — BuildPromptFor sin test directo → correcto, no útil
- **#572**
  - menor, NV — truncado → sin decidir, no útil
  - menor, NV — truncado → sin decidir, no útil
  - menor, NV — truncado → sin decidir, no útil
- **#571**
  - menor, NV — truncado → sin decidir, no útil
- **#570**
  - menor, NV — lastFailureBlock: cubierto por test de engine → falso, no útil
  - menor — regla 29: cambia el prompt y no se pegó una muestra → correcto, útil
- **#566**
  - menor, NV — truncado → sin decidir, no útil
- **#565**
  - menor, NV — truncado → sin decidir, no útil
  - menor, NV — truncado → sin decidir, no útil
- **#564**
  - bloqueante — rama de recorte de la descripción sin test (el test afirma lo contrario) → correcto, útil
  - bloqueante — login vacío en IsCollaborator sin test → correcto, no útil
- **#563**
  - bloqueante — RowsAffected descartado -> 6a106a0 → correcto, útil, atendido
  - bloqueante — rama sin intentos de writeMetrics sin test -> 6a106a0 → correcto, útil, atendido
  - bloqueante — regla 29: salida de orch metrics pegada en el cuerpo → correcto, útil, atendido
  - menor — rows.Err sin envolver -> 6a106a0 → correcto, no útil, atendido
  - bloqueante — ramas Stopped/TimedOut/approved de attemptRow sin test -> d560b63 → correcto, útil, atendido
  - menor, NV — rama s.state==nil → sin decidir, no útil
- **#560**
  - bloqueante — defer _ = closeDB(): idiom usado 32 veces en el repo → correcto, no útil
  - bloqueante — idem → correcto, no útil
  - bloqueante — ParseInt "descartado": se devuelve con exit 2 → falso, no útil
  - bloqueante — defer _ = closeDB() en un test → correcto, no útil
  - menor, NV — truncado → sin decidir, no útil
  - menor, NV — truncado → sin decidir, no útil
  - menor, NV — truncado → sin decidir, no útil
- **#558**
  - menor, NV — test en doctor.txtar no visible → sin decidir, no útil
- **#557**
  - menor, NV — badge stalled en TaskCard sin test → correcto, no útil
  - menor, NV — icono stalled en ListPage sin test → correcto, no útil
- **#553**
  - menor, NV — validación de negativo sin test → correcto, no útil
- **#517**
  - menor — título viejo del evento (re-run usa el payload original) → falso, no útil
- **#515**
  - menor, NV — (ídem) → sin decidir, no útil
  - menor, NV — (ídem) → sin decidir, no útil
  - menor — regla 29: columna KIND sin salida pegada → correcto, no útil
- **#514**
  - menor, NV — validación de negativo sin test → correcto, no útil
  - menor — Statfs sin envolver → correcto, no útil
- **#510**
  - bloqueante — "URL de repo privado": billing-api es público → falso, no útil
  - bloqueante — idem en el test → falso, no útil
  - menor, NV — (ídem) → sin decidir, no útil
  - menor — regla 20: tres funciones en vez de tabla → correcto, no útil
  - menor — regla 29: mensaje de bloqueo no pegado → correcto, no útil
- **#502**
  - menor, NV — install.sh sin test POSIX en CI de PR → correcto, no útil
- **#496**
  - bloqueante — time.Sleep en polling -> f29169f → correcto, útil, atendido
  - bloqueante — exec de claude sin Setpgid ni timeout -> f29169f → correcto, útil, atendido
  - bloqueante — ruta de ejemplo bajo /home en un test web -> f29169f → correcto, no útil, atendido
  - menor, NV — wantsWelcome → sin decidir, no útil
  - bloqueante — regla 29: salida pegada en el cuerpo → correcto, útil, atendido
  - bloqueante — git init con %v -> 93bc7f5 → correcto, no útil, atendido
  - bloqueante — run.Wait descartado -> 93bc7f5 → correcto, útil, atendido
  - bloqueante — rama Run sin test -> 192514e → correcto, útil, atendido
  - menor — errores sin envolver en Project/Apply → sin decidir, no útil
  - menor — test no visto fallar → sin decidir, no útil
- **#493**
  - bloqueante — sandboxPolicy/gitCommonDir sin test -> 49c822e → correcto, útil, atendido
  - bloqueante — sandbox-exec sin test -> 49c822e → correcto, útil, atendido
  - bloqueante — rama Sandbox!=nil en spawn sin test -> 49c822e → correcto, útil, atendido
  - menor — ABI con %v -> 5884def → correcto, no útil, atendido
  - menor — regla 29 -> salida de doctor pegada → correcto, útil, atendido
  - menor, NV — regla 18 en spawn: Setpgid se conserva (test lo afirma) → falso, no útil
  - menor, NV — wiring en scheduler → sin decidir, no útil
  - menor — regla 23: EvalSymlinks sin comentario → correcto, no útil
  - menor — regla 29 repetida cuando el cuerpo ya la tenía → falso, no útil
- **#491**
  - bloqueante — tuiLoop sin test -> c5e6e3f → correcto, útil, atendido
  - bloqueante — buildStatusRows sin envolver -> c5e6e3f → correcto, no útil, atendido
  - bloqueante — ReadActivity descartado -> c5e6e3f → correcto, útil, atendido
  - bloqueante — term.Restore descartado -> c5e6e3f → correcto, útil, atendido
  - menor, NV — regla 30: sí hidrata (comentario agregado) → falso, no útil, atendido
- **#490**
  - menor — regla 24: dos tests no vistos fallar (lo dice el cuerpo) → correcto, no útil
  - menor — idem → correcto, no útil
- **#489**
  - bloqueante — readTail: continue a propósito (mejor esfuerzo) → correcto, no útil
  - bloqueante — os.Open sin envolver (PathError ya trae la ruta) → correcto, no útil
  - bloqueante — defer f.Close() de solo lectura → correcto, no útil
  - bloqueante — Stat sin envolver → correcto, no útil
  - bloqueante — ReadAt sin envolver → correcto, no útil
- **#488**
  - bloqueante — contraseña falsa en el test del guard: el escáner necesita un secreto falso → correcto, no útil
  - bloqueante — patrones AWS/Slack/OpenAI/... sin test → correcto, útil
  - bloqueante — guard on por defecto cambia proyectos existentes (regla 2) → correcto, útil
  - menor, NV — Manager.Diff sin test en el PR → correcto, no útil
  - menor — regla 20: tabla → correcto, no útil
- **#479**
  - bloqueante — time.Sleep en test -> 1a40171 → correcto, útil, atendido
