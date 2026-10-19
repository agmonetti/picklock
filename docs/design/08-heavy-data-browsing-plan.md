# 08 — Plan iterativo: navegación segura y columnas pesadas

Issue: [#1 — Mejorar rendimiento y estabilidad al explorar tablas con columnas pesadas](https://github.com/agmonetti/picklock/issues/1).

Este documento es el plan vigente para ese issue, no un registro de funcionalidades ya implementadas. Complementa el plan histórico de construcción de Picklock. Para los motores actualmente soportados prevalecen el código y la documentación de `../non-relational/`; este trabajo no elimina motores existentes.

## Seguimiento de ejecución

**Etapa activa: E7 — Validación integral y cierre.**

Actualizado: 2026-10-15. E1–E6 FINALIZADAS y verificadas; E7 ACTIVA.

| Etapa | Estado | Depende de | Resultado requerido |
|---|---|---|---|
| E0 — Diagnóstico y decisiones | FINALIZADA | — | Problema localizado, evidencia inicial y diseño de referencia |
| E1 — Renderizado con trabajo acotado | FINALIZADA | E0 | Dibujar una celda no recorre repetidamente su contenido completo |
| E2 — Contratos y preferencias | FINALIZADA | E1 | Opciones de exploración, estados de celda, identidad y límites explícitos |
| E3 — Navegación segura en drivers SQL | FINALIZADA | E2 | Proyecciones acotadas y paginación sin descargar payloads completos |
| E4 — Apertura y opciones de exploración | FINALIZADA | E3 | Aviso contextual y controles del usuario antes del primer fetch |
| E5 — Visor bajo demanda, copia y exportación | FINALIZADA | E4 | Acceso completo cancelable sin cargar el valor entero en la grilla |
| E6 — Protección del editor SQL | FINALIZADA | E5 | Presupuestos de filas/bytes y resultados incompletos identificables |
| E7 — Validación integral y cierre | ACTIVA | E6 | Matriz de motores, métricas y aceptación del caso reportado |

### Reglas para cada iteración

1. Mantener exactamente una etapa `ACTIVA` hasta completar el plan. Al cerrar E7, todas deben quedar `FINALIZADA` y la etapa activa será `ninguna`.
2. Trabajar en orden. Una etapa activa indica el foco actual; no significa que sus tareas estén implementadas.
3. Marcar `[x]` sólo después de realizar la tarea. Marcar la etapa `FINALIZADA` sólo cuando se cumplan sus criterios de cierre y exista evidencia registrada.
4. En la misma actualización que cierra una etapa, activar la siguiente y cambiar el encabezado de etapa activa. No reabrir etapas cerradas para esconder regresiones: registrar la corrección y su impacto.
5. Si falta un recurso externo, marcar la etapa `BLOQUEADA`, describir qué falta y conservar un único foco explícito. No presentar una verificación no ejecutada como aprobada ni saltar dependencias.
6. Después de cada iteración, actualizar tareas, estado, bitácora, verificación y siguiente acción. Las decisiones nuevas deben registrar motivo y efecto sobre el alcance.
7. Los cambios de interfaz incluyen todos sus consumidores, pruebas y documentación; no dejar aliases o rutas obsoletas para evitar la migración.
8. Verificar una vez estabilizado cada cambio de etapa, no ejecutar build/tests tras cada edición. El smoke del comportamiento real es obligatorio además de las pruebas pertinentes.

**Siguiente acción concreta:** completar E7 con las fixtures/matriz funcional pendientes y conseguir el contenedor representativo `product_images`; no extrapolar el smoke/benchmark sintético a ese caso.

## Objetivo y límites del trabajo

### Resultado para el usuario

- Explorar `product_images` y otras tablas sin congelar la TUI por valores TEXT/BLOB/JSON grandes.
- Poder recorrer todas las filas por páginas o acotar columnas, filtros y tamaño de página antes de cargar datos.
- Abrir un campo completo bajo demanda y guardarlo, con progreso y cancelación.
- Distinguir datos completos, vistas previas, valores diferidos, `NULL` y resultados incompletos.
- Mantener memoria proporcional a la página/vista retenida, no al total de payloads de la tabla.

### Alcance

- Proyección segura y lectura bajo demanda: MySQL, MariaDB, PostgreSQL, SQLite y SQL Server.
- Renderizado compartido y presupuestos del editor: preservar los formatos tabular, documento, clave-valor, grafo y texto ya soportados.
- MongoDB, Redis, Cassandra y Neo4j conservan sus protocolos de navegación. No reciben filtros SQL ni una falsa garantía de carga diferida en servidor; la proyección específica de esos motores no forma parte de esta adaptación relacional.
- Reutilizar Bubble Tea, drivers, metadata, paginación, preferencias y cancelación existentes. No agregar dependencias externas para este trabajo.
- Fixtures sólo en bases desechables. Contra la base reportada, todas las operaciones de validación serán de lectura.

### No se implementará

- Una opción de descargar toda la tabla automáticamente a memoria.
- Un ORM, un parser SQL nuevo o un sistema general de consultas distribuido.
- Una caché de páginas o LRU: se conserva la página activa y lo estrictamente necesario para navegación/operaciones en curso.
- Reescritura arbitraria o reejecución automática del SQL escrito por el usuario.
- Una promesa de memoria total estrictamente acotada para cualquier consulta arbitraria: el driver puede asignar una celda antes de que Picklock pueda limitarla.

## Decisiones de diseño

### Apertura de tablas

1. Cargar estructura/capacidades antes de recuperar filas. Separar la preparación de la apertura del fetch, también para la primera tabla al conectar.
2. Sin campos potencialmente pesados, abrir directamente con los límites seguros.
3. Con campos potencialmente pesados, mostrar antes del primer fetch: tipos detectados y acciones `Explorar`, `Acotar`, `Cancelar`.
4. `Explorar` permite recorrer todas las filas por páginas, sin recuperar campos pesados completos.
5. `Acotar` permite elegir columnas, filtros estructurados y filas por página. Aplicar inicia la carga; cancelar conserva la vista anterior.
6. Mostrar el aviso una vez por tabla y conexión durante la sesión. Rehabilitarlo si cambia la estructura relevante. Las opciones se pueden abrir después sin reconectar.
7. Un fallo de metadata no habilita silenciosamente un `SELECT *` inseguro. Mostrar el error y conservar la vista anterior.
8. No usar `COUNT(*)`, escaneo de valores ni suma de longitudes para decidir el aviso. Cuando no hay un total barato, mostrar `total desconocido` y usar lookahead para `HasNext`.
9. No describir una tabla como lenta sólo por su tipo o número estimado de filas. El mensaje explica el riesgo conocido sin afirmar una medición inexistente.

### Parámetros iniciales

| Preferencia | Valor inicial | Uso |
|---|---|---|
| `browse_page_size` | 50 filas | Tamaño de página; configurable también al abrir |
| `preview_bytes` | 256 bytes | Máximo de texto retenido como vista previa, sin cortar caracteres UTF-8 |
| `value_block_bytes` | 64 KiB | Bloque de lectura/visualización del campo completo |
| `editor_max_cell_bytes` | 64 KiB | Representación retenida por celda del editor; truncado explícito |
| `editor_max_result_bytes` | 16 MiB | Presupuesto acumulado de contenido retenido por resultado |
| `editor_max_rows` | 10.000 filas | Mantiene el límite actual, haciéndolo configurable |
| `query_timeout_seconds` | Preferencia existente, 60 s por defecto | Contexto de operaciones DB, incluida carga bajo demanda |

- Exponer los límites en la pantalla de ajustes existente y persistirlos en `Prefs`; no crear otro sistema de configuración.
- Rechazar valores no positivos, desbordamientos y combinaciones incoherentes; los archivos antiguos reciben los valores por defecto. No ofrecer `0 = ilimitado` para desactivar protecciones por accidente.
- `preview_bytes` no puede superar `value_block_bytes`; `editor_max_cell_bytes` no puede superar `editor_max_result_bytes`. Un cambio de opciones reinicia los cursores afectados.
- Los presupuestos describen bytes de contenido retenido, no RSS total: estructuras Go, driver, conversiones y operaciones en curso tienen overhead adicional.

### Contratos de datos

- Extender `BrowseRequest` con opciones de columnas, filtros y límites. Mantener la selección de dialecto y la generación de SQL en los drivers, no en la TUI.
- Filtros: columna del metadata, operador permitido (`=`, `!=`, `<`, `<=`, `>`, `>=`, `IS NULL`, `IS NOT NULL`) y valor tipado; combinación `AND`. No aceptar fragmentos SQL libres en ese formulario.
- Conservar una sola copia del contenido de cada celda. `Rows` contiene valores completos o prefijos; metadata separada identifica sólo los valores incompletos y su tipo/tamaño conocido. `Nulls` sigue siendo la fuente de verdad para `NULL`.
- No insertar placeholders dentro de `Rows`. La UI deriva `<LONGTEXT · diferido>` o una vista previa de la metadata. Un tamaño desconocido no se representa como cero.
- Cada fila navegable puede llevar una referencia opaca al driver con la clave tipada, independiente de las columnas visibles y del string usado para dibujarla.
- PK simple/compuesta o índice único con todas sus columnas no nulas identifica la fila. No reconstruir claves a partir de previews, hexadecimal de presentación o texto truncado.
- Introducir una capacidad explícita de lectura de celdas para los orígenes que la implementan. No obligar a los motores no relacionales a declarar métodos ficticios.
- Tablas sin clave fiable: navegación segura disponible; recuperación puntual deshabilitada con explicación. Nunca usar posición visual, OFFSET o `LIMIT 1` como identidad. El editor SQL sigue disponible.
- Aplicar contratos equivalentes de incompletitud y presupuestos a los resultados del editor, incluidos formatos no tabulares.

### SQL y carga acotada

- Sustituir `SELECT *` por columnas/expresiones explícitas. Campos no seleccionados no se recuperan, salvo claves internas necesarias.
- Campos pesados: diferir el valor. Texto variable no pesado: recuperar un prefijo limitado en servidor. No ocultar indiscriminadamente todo `TEXT` pequeño.
- Clasificación por metadata y reglas del dialecto; contemplar tipos sin longitud, aliases, JSON/binarios y tipos definidos por el usuario cuando el driver exponga su tipo base.
- Para límites en bytes, tener en cuenta que `SUBSTRING` puede contar caracteres y que un carácter multibyte ocupa varios bytes. La expresión SQL debe tener un límite de transferencia finito; el prefijo retenido debe respetar `preview_bytes` sin romper UTF-8.
- SQLite requiere además tratar su tipado dinámico: una declaración numérica no garantiza que el valor almacenado no sea texto/blob grande.
- No calcular el tamaño de todos los campos pesados obligatoriamente en cada página. Mostrar tipo y estado; obtener tamaño exacto al abrir el campo, o cuando el dialecto pueda incorporarlo con coste aceptable medido.
- Mantener keyset donde sea aplicable y offset como alternativa. Determinar `HasNext` pidiendo una fila adicional, aun cuando se desconozca el total. No retener ni convertir en detalle el payload de la fila adicional.
- Los filtros se aplican en DB. Identificadores validados contra metadata y citados por dialecto; valores parametrizados. Las consultas generadas de exploración/visor son de lectura.
- Sin clave/orden estable, no prometer navegación como snapshot frente a escrituras concurrentes; informar esa limitación sin bloquear toda exploración.

### Visor, consistencia y ciclo de vida

- `Abrir valor` es una acción explícita desde el detalle. Su trabajo DB/serialización se ejecuta en background; la UI sólo presenta un bloque acotado.
- Para obtener un valor completo, leer fragmentos acotados desde el servidor y materializar a disco temporal privado. No leer primero la celda completa para después dividirla en Go.
- Mantener una vista consistente del campo durante la materialización, con transacción/snapshot de lectura apropiada al motor. Finalizarla al terminar/cancelar la descarga, no mantenerla abierta durante toda la interacción del usuario con el visor.
- Si el motor no puede establecer una lectura consistente con los permisos actuales, informar el impedimento; no combinar silenciosamente bloques de versiones distintas. No modificar ajustes globales ni activar opciones administrativas automáticamente.
- La representación completa debe preservar los bytes de binarios y una representación textual documentada para texto/JSON. La vista hexadecimal o decodificada es presentación, no el archivo original.
- Progreso por bytes recibidos; porcentaje sólo cuando exista un tamaño fiable. El contenido parcial debe figurar como tal.
- Leer bloques del archivo para navegar; no cargar ni parsear el JSON completo para mostrarlo. Para texto, conservar correctamente los límites multibyte entre bloques; para binarios, presentar una vista acotada.
- Usar los tokens de operación/sesión existentes para descartar respuestas tardías. Esc, cambio de tabla, desconexión y salida cancelan operaciones relacionadas y cierran recursos.
- Temporales con permisos privados, sin payloads en logs; eliminar al cerrar/cancelar y también ante errores. Durante descarga, el disco puede crecer con el campo; errores de espacio se muestran sin bloquear ni dejar el resultado como completo.

### Copia y exportación

- Copiar un valor incompleto exige una elección explícita: copiar la vista previa indicada como tal o recuperar el valor completo. Nunca copiar un placeholder como dato.
- Clipboard completo sólo dentro del límite de celda configurado; para valores mayores ofrecer guardar a archivo. Invocar el portapapeles también puede crear copias grandes.
- Copiar una fila completa o exportar una página con campos diferidos exige resolverlos mediante streaming de lectura, o rechazar la operación con explicación; no omitirlos, sustituirlos por `NULL` ni exportar marcadores silenciosamente.
- La exportación existente continúa siendo del resultado/página seleccionada, no un nuevo dump de toda la tabla. Si una fila no tiene identidad fiable y requiere recuperar datos, explicar por qué no puede completarse.
- Para una exportación completa de la página, usar una lectura consistente del conjunto solicitado; no mezclar claves antiguas con bloques de diferentes versiones sin advertirlo. Cancelar no deja un archivo parcial presentado como terminado.

### Editor SQL

- Conservar exactamente la sentencia elegida por el usuario, la ejecución en background y la distinción entre lectura y mutación existente.
- Aplicar límites de filas, representación por celda y bytes acumulados antes de retener/copiar más contenido del necesario. Contar la representación final: un binario en hexadecimal ocupa más que su payload.
- Cuando se alcanza el límite total, devolver sólo filas/items admitidos e indicar causa del corte, filas retenidas y bytes de contenido. Una primera fila que no cabe produce un aviso, no un falso resultado vacío completo.
- La paginación visual opera sobre el resultado retenido. No presentar el corte como si existiera un cursor DB que permite obtener automáticamente el resto de cualquier SQL arbitrario.
- Un valor de una expresión arbitraria no obtiene mágicamente identidad de fila. No reejecutar la consulta para abrirlo o exportarlo completo sin intervención del usuario.
- Cerrar filas/cursor al finalizar o cortar. Medir el comportamiento real del driver: limitar retención no garantiza evitar la asignación/transferencia inicial de una celda ni el drenaje de resultados al cerrar.

## E0 — Diagnóstico y decisiones

**Estado: FINALIZADA.**

### Tareas realizadas

- [x] Leer el issue y registrar sus tres criterios principales: navegación responsiva, memoria razonable y contenido completo bajo demanda.
- [x] Verificar que ya existen paginación DB, comandos asíncronos, contexto, timeout y cancelación.
- [x] Localizar `SELECT *` en las dos rutas de paginación MySQL y la conservación completa de valores en `internal/store/scan.go`.
- [x] Confirmar que el detalle actual muestra valores ya cargados, sin recuperación puntual.
- [x] Localizar el truncado cuadrático y el cálculo de anchos sobre celdas completas.
- [x] Ejecutar un smoke sintético de la función real `RenderMainBrowser`, sin tocar bases existentes; eliminar el programa temporal.
- [x] Contrastar el diseño con controles documentados de Workbench, DBeaver y DataGrip.

### Evidencia inicial

Una fila, celda de texto ASCII repetido, ventana de 80 columnas por 10 líneas. Medición local de una ejecución; no es una distribución estadística ni un benchmark MySQL.

| Payload de la celda | Tiempo de renderizado | Asignaciones acumuladas |
|---|---:|---:|
| 1 KiB | 11,013 ms | 2,66 MiB |
| 4 KiB | 179,468 ms | 42,36 MiB |
| 8 KiB | 702,176 ms | 168,96 MiB |

Las asignaciones son `TotalAlloc` durante la llamada, no RSS ni heap vivo. La salida tuvo el mismo tamaño en los tres casos. La contribución exacta de cada problema al caso MySQL reportado sigue pendiente de medición; el congelamiento reportado es un hecho, no algo que deba cuestionarse con una nueva reproducción.

## E1 — Renderizado con trabajo acotado

**Estado: FINALIZADA.** Dependencia: E0.

Archivos principales: `internal/tui/screens/browser.go`, sus pruebas y consumidores de los helpers compartidos; revisar también el detalle y las vistas de resultados.

### Tareas

- [x] Reemplazar `truncate` por recorte de prefijo limitado al ancho visual, con elipsis incluida en el presupuesto; reutilizar las bibliotecas ya presentes si respetan las reglas actuales de Unicode/ANSI.
- [x] Eliminar el bucle de borrar la última runa y sus conversiones completas repetidas; retirar el helper obsoleto si queda sin consumidores.
- [x] Hacer que el cálculo de ancho termine al alcanzar el máximo de columna; no calcular primero el ancho completo para limitarlo después.
- [x] Mantener cabeceras, selección, `NULL`, scroll horizontal, alineación y ventanas de filas visibles.
- [x] Revisar todos los consumidores mediante referencias LSP antes de cambiar helpers exportados o contratos.
- [x] Añadir regresiones de comportamiento para ancho exacto, ancho mínimo, multibyte, caracteres de ancho doble y combinaciones; no fijar tiempos frágiles en unit tests.
- [x] Añadir un benchmark de la ruta real de renderizado con tamaños crecientes y salida visual equivalente; registrar tiempo y asignaciones.
- [x] Ejecutar el smoke sintético y lanzar la TUI con una fixture desechable para observar navegación, scroll y redimensionado con celdas grandes.
- [x] Actualizar la documentación de comportamiento/rendimiento afectada tras verificar el cambio.

### Cierre

- La salida cabe en su ancho y conserva el contenido visible correcto.
- Multiplicar el sufijo oculto no dispara de nuevo el coste cuadrático ni asignaciones proporcionales a cada eliminación de carácter.
- La interacción de la TUI sigue funcionando con valores grandes ya presentes en memoria.
- Registrar comando, tamaños, tiempos y asignaciones posteriores comparables con E0. No afirmar todavía que se corrigió la transferencia MySQL.

### Evidencia E1 — 2026-10-15

**Implementación y decisiones.** `internal/tui/screens/browser.go` recorta por
grafemas y mide sólo hasta el máximo visual. Se reutiliza `uax29/v2 v2.5.0`,
ya presente transitivamente, promovida a dependencia directa; no se incorpora
ninguna biblioteca nueva. Se conserva la política de ancho de `runewidth` y
los estilos ANSI se aplican después del recorte. LSP revisó referencias de
`truncate`, `WrapCells`, `RenderRowDetail` y `RenderDataTableWithNulls`, incluido
el editor. No cambian firmas exportadas ni contratos de datos. Se eliminaron
`trimLastRune`, `colWidths` y `renderHeader`; los últimos dos no tenían
consumidores productivos, y se retiraron las pruebas exclusivas del helper muerto.
Documento/grafo acotan valores antes de formatear; texto/KV limitan las líneas
presentadas. El detalle envuelve hasta el final del viewport mediante slices,
sin construir el sufijo oculto completo.

**Comandos ejecutados.**

- `gofmt -w internal/tui/screens/browser.go internal/tui/screens/browser_test.go`.
- `go test ./internal/...`: 17 paquetes aprobados y 2 sin pruebas. Incluye
  regresiones de elipsis dentro del presupuesto, ancho exacto/mínimo, UTF-8,
  ancho doble, combinaciones/ZWJ, alineación, NULL/vacío, selección y ventanas.
  Esto no acredita servicios de red no configurados.
- `go test ./internal/tui/screens -run '^$' -bench '^BenchmarkRenderMainBrowserHeavyCell$' -benchmem -count=3`: aprobado;
  cada tamaño produjo exactamente la misma salida. Linux/amd64,
  Go `go1.27.2-X:nodwarf5`, Intel Core i7-10510U, GOMAXPROCS 8 (sufijo del benchmark).
- `go build -o /tmp/picklock-e1-smoke ./cmd/picklock/`: aprobado.
- `go vet ./...`: aprobado, sin diagnósticos.
- `go run ./.e1-smoke/`: programa temporal llamando al `RenderMainBrowser`
  público, una fila ASCII, ventana 80×10 como E0, entrada creada antes de medir,
  GC antes de cada llamada. Primera ejecución aislada registrada abajo. Salida
  idéntica de 113 bytes en todos los tamaños; sin color al no haber TTY.
  Una segunda ejecución ejercitó también documento, grafo, texto y KV con
  payload de 1 MiB, comprobando prefijo, elipsis, ancho y líneas visibles.
  Programa temporal retirado después del smoke.

| Celda | Smoke aislado | TotalAlloc del smoke | Benchmark mediana (3 repeticiones) | B/op del benchmark | allocs/op |
|---|---:|---:|---:|---:|---:|
| 1 KiB | 41,331 µs | 1.184 B | 10.625 ns | 2.088–2.089 | 59 |
| 4 KiB | 29,008 µs | 1.200 B | 9.511 ns | 2.088 | 59 |
| 8 KiB | 14,858 µs | 1.184 B | 9.094 ns | 2.088 | 59 |
| 1 MiB | 12,896 µs | 1.184 B | 9.211 ns | 2.089 | 59 |
| 16 MiB | 22,392 µs | 1.184 B | 9.039 ns | 2.088 | 59 |

`TotalAlloc` no es heap vivo ni RSS. El benchmark fuerza TrueColor mediante
el setup de pruebas existente, por eso no se igualan sus bytes con el smoke
sin color. Las diferencias de tiempo entre tamaños son ruido de medición local,
no una promesa de latencia. Frente a E0 no reaparece crecimiento cuadrático
ni asignaciones por cada carácter descartado.

**Smoke de TUI real.** Ejecutado
`go run ./cmd/picklock/ --read-only /tmp/picklock-e1-titkrd8s/heavy.sqlite`
en PTY con `TERM=xterm-256color` y configuración XDG aislada. Fixture SQLite
desechable creada con `sqlite3`: tabla `heavy(id INTEGER PRIMARY KEY,
payload TEXT, optional TEXT, tail TEXT)`, 74 filas; payload
`printf('row-%03d-',i) || replace(hex(zeroblob(524288)), '0', 'x')`.
Cada celda mide 1.048.584 bytes; total de payload 77.595.216 bytes.
`optional` alterna NULL y vacío; segunda tabla con `é界👩‍💻`.

Se observaron cabeceras, elipsis, distinción NULL/vacío y fondo de selección
en la fila siguiente; `g`/`G` movieron la ventana hasta `row-050` y de vuelta.
`Ctrl+D`/`Ctrl+U` avanzaron a las 24 filas finales (`row-051` en adelante)
y regresaron a las 50 iniciales. Redimensionado real con `TIOCSWINSZ`/`SIGWINCH`
de 120×32 a 72×24, seguido de `Alt+L`/`Alt+H`, mostró indicadores y columnas
sin bloquear la entrada. `v`, selección de payload y `Esc` abrieron/navegaron/
cerraron el detalle grande. La consulta real
`SELECT id, payload, optional FROM heavy WHERE id <= 2;` ejecutada con `Ctrl+R`
mostró ambas filas en el editor. Se abrió la tabla Unicode y se verificó la
secuencia completa en los bytes del terminal; el capturador `pyte` no compone
visualmente el emoji ZWJ. `Ctrl+C` terminó con código 0. Fixture, binario y
herramientas temporales retirados; ninguna base existente fue modificada.

**Límites observados.** No se cambian fetch, retención de valores completos,
copia/exportación ni presupuestos del editor. MySQL/product_images y los demás
servicios no se ejercitaron en E1; siguen pendientes para sus etapas. El detalle
aún cuenta campos completos al navegar y recorre el prefijo para scroll profundo;
seleccionar un campo alto desplaza al final y puede ocultar su cabecera.
Texto busca separadores de línea; una secuencia enorme de ancho cero/un único
grafema también puede requerir escaneo largo. No se promete coste constante
para todo formato/input, reducción de RSS ni transferencia segura.
Documentación actualizada: `USAGE.md`, `03-ux-screens.md` y
`05-technical-decisions.md` (se corrigió la afirmación obsoleta de caché de anchos).
Revisión arquitectónica y de cierre documental realizada: sin hallazgos
bloqueantes; incorporadas precisiones de atajos y límites visuales.

## E2 — Contratos y preferencias

**Estado: FINALIZADA.** Dependencia: E1.

Archivos principales: `internal/store/store.go`, `internal/store/relational.go`, `internal/browser/browser.go`, `internal/prefs/prefs.go`, `internal/tui/screens/settings.go` y consumidores.

### Tareas

- [x] Introducir opciones de exploración y filtros estructurados, estados de incompletitud, referencias de fila tipadas y capacidad de lectura de celdas según las decisiones anteriores.
- [x] Definir un único contrato de presupuestos para ejecución del editor; migrar `QueryExecutor.Execute` y todos los motores/callers, incluido MCP si consume los contratos cambiados.
- [x] Adaptar resultados/exportadores para que un dato incompleto no parezca completo. Evitar duplicar contenido o dos fuentes de verdad para `NULL`.
- [x] Separar preparación/inspección de una tabla de la carga de su primera página.
- [x] Incorporar preferencias y ajustes con defaults/migración de archivos existentes; opciones de tabla sólo en la sesión, sin guardar filtros o datos sensibles por defecto.
- [x] Conservar capacidades reales de los motores no relacionales; rechazar opciones no soportadas explícitamente en vez de ignorarlas.
- [x] Cubrir límites, desbordamientos, `NULL` frente a vacío, previews y pérdida de identidad por truncado en pruebas de comportamiento.
- [x] Ejercitar opciones y resultados con SQLite desechable y observar en la TUI el guardado/restauración de ajustes.

### Cierre

- Todos los consumidores usan el contrato nuevo; sin adaptadores ficticios ni métodos no-op para motores no compatibles.
- Una celda diferida o parcial no puede confundirse con un valor completo al copiar/exportar.
- Cambiar opciones reinicia cursores y no reaprovecha resultados de otra selección.
- Los archivos de preferencias anteriores siguen cargando con límites seguros.

### Evidencia E2 — 2026-10-15

**Contratos y migración.** `internal/store/browsing.go` define `BrowseOptions`,
filtros AND con operadores permitidos/valores canónicos tipados, validación de
columnas contra metadata y aritmética de páginas sin overflow. SQL y motores
no relacionales rechazan explícitamente columnas/filtros/previews todavía no
implementados; no se hace filtrado local. `QueryLimits` unifica filas/celda/
resultado y migra los cinco ejecutores, editor, TUI, MCP y sus callers/pruebas.
MCP conserva 500 filas; editor usa preferencias (10000 por defecto). E2 aplica
los topes de filas existentes; aplicar bytes durante lectura/conversión es E6.

`TabularData.Rows` conserva una sola representación completa/prefijo; metadata
dispersa sólo para incompletos y `Nulls` autoritativo. Documentos, KV, grafo y
texto tienen metadata equivalente y `ResultStatus` para futuros productores.
Tamaños desconocidos usan nil; `Truncated` legacy no inventa bytes retenidos.
`RowReference` conserva claves nativas compuestas, copia sus bytes para impedir
aliasing con scan buffers y no depende de strings visibles; `RowRef` devuelve
nil sin identidad. `CellReadRequest` valida referencia/columna/bloque;
`CellReader` es opcional, sin implementaciones ficticias. NULL no tiene reader.
Drivers producirán identidades/proyecciones reales en E3 y lecturas en E5.

**Consumo seguro.** `store/completeness.go` centraliza estado/avisos; grilla,
editor, detalle y MCP no reescriben payloads con placeholders. Copia de campo
completo sigue disponible; copia parcial/de detalle incompleto se rechaza con
explicación. La selección visual tampoco copia marcadores como datos. CSV/JSON
rechazan incompletitud/cortes antes de escribir bytes; los formatos JSON
completos existentes y la distinción NULL/vacío se conservan. La política E2
es conservadora: elección explícita de preview y recuperación completa son E5.

**Preparación y preferencias.** `Browser.New` carga sólo catálogo;
`PrepareItem` inspecciona sin fetch ni reemplazo de vista; `OpenPrepared`
recibe opciones/page size explícitos antes del primer fetch y sólo confirma
en éxito. `SetOptions` reinicia toda la historia de cursores y conserva la
selección anterior ante error. Se retiró el alias `SelectTable`, migrando
consumidores. `Clone` comparte payloads publicados inmutables, sin duplicar
páginas. La TUI compone preparación/carga en background; aviso/formulario
de exploración siguen siendo E4.

Seis preferencias nuevas más timeout en el formulario existente, con Tab/
Shift+Tab/↑↓, validación positiva/overflow y relaciones preview≤block,
cell≤result. Archivos viejos reciben defaults; inválidos devuelven error y
defaults seguros; Save inválido no pisa el archivo. No se persisten filtros/
datos. `browse_page_size` es default de nuevas aperturas/conexiones: guardar
ajustes no recarga la selección activa; cambiar sus opciones usa `SetOptions`.
Revisión arquitectónica realizada; incorporadas propiedad de identidad,
accessor seguro, opciones antes del primer fetch y validación de lectura.

**Verificaciones ejecutadas.**

- `gofmt -w` sobre archivos Go modificados.
- `go test ./...`: aprobado (18 paquetes con pruebas, 4 sin pruebas). Regresiones
  de límites/overflow, filtros inválidos, claves nativas/aliasing, cambio de
  cursores, metadata fallida, NULL/vacío, previews/diferidos, cortes vacíos,
  formatos no tabulares, copia, exportación sin bytes y prefs antiguas/guardado.
- `go build -o /tmp/picklock-e2-_2cebpp2/picklock ./cmd/picklock/`: aprobado.
- `go vet ./...`: aprobado, también tras la corrección visual descrita abajo.
- `go run ./.e2-smoke/ /tmp/picklock-e2-_2cebpp2/fixture.sqlite`: aprobado,
  utilizando DataSource/Browser/QueryExecutor/export reales en modo read-only.
  Fixture desechable: 12 filas `heavy(id INTEGER PRIMARY KEY, payload TEXT,
  optional TEXT, binary_key BLOB)`, payload `row-NNN-` más `界é` repetido 512
  veces; optional alterna NULL/vacío y bytes binarios `00 ff id`.
  Páginas 4→3 reinician a id=1, siguiente id=4 sin cursor anterior; opciones
  no soportadas fallan sin reemplazar vista. Consulta con límite 2 devuelve
  dos filas/corte y binario `0x00ff01`; JSON cortado rechaza sin bytes y JSON
  completo conserva null frente a `""`.

**Smoke de TUI real.** Binario anterior en PTY, `TERM=xterm-256color`,
`--read-only /tmp/picklock-e2-_2cebpp2/fixture.sqlite` y `PICKLOCK_CONFIG_DIR`
aislado. Inicio con archivo viejo sólo timeout; se observaron los siete
defaults. Se editaron/guardaron 90 s, página 4, preview 128, bloque 8192,
celda 4096, resultado 65536 y filas editor 2; `prefs.json` real comprobado.
Reapertura y nuevo proceso restauraron los siete valores. Preview 9000 con
bloque 8192 fue rechazado manteniendo el formulario; Esc no guardó el inválido.
Nueva conexión mostró ids 1–4; Ctrl+D ids 5–8 y Ctrl+U ids 1–4. SQL real
`SELECT id, optional, binary_key FROM heavy ORDER BY id;` retuvo dos filas.
Alt+E/Enter rechazó exportar el corte y no creó `partial.csv`.

El primer smoke detectó que el aviso del editor quedaba debajo del viewport
corto. Se corrigió reutilizando la línea del separador para el estado, sin
duplicarlo; el smoke final mostró aviso y ambas filas en el panel normal.
Se añadió regresión para alturas 6/7/10. La prueba de scroll necesitó una
fixture de cinco filas para mantener un rango desplazable con el espacio
recuperado (la fixture anterior de tres ya cabía completa).
`go test ./internal/tui/screens ./internal/tui` y rebuild posteriores:
aprobados. Resize real a 80×20 más Shift+Tab mantuvo visible el último ajuste.
Las tres ejecuciones TUI terminaron con código 0. Programa Go, fixture,
configuración, binario y herramientas temporales retirados.

**Límites y documentación.** Sin hosts `PICKLOCK_TEST_*_HOST` configurados;
no se acredita integración con servicios de red ni el caso `product_images`.
No se cambió proyección/transferencia/conteo SQL, no hay visor ni aplicación
de presupuestos de bytes: son E3/E5/E6 respectivamente. Los tests ejercitan
metadata sintética de previews; los drivers aún no la producen. No se midió
RSS ni rendimiento de red en E2. Actualizados README, USAGE, arquitectura,
decisiones técnicas y contrato no relacional. Sin publicación en GitHub.

## E3 — Navegación segura en drivers SQL

**Estado: FINALIZADA.** Dependencia: E2. Criterios de cierre verificados; evidencia abajo.

Archivos principales: `internal/store/relational.go`, `internal/store/scan.go`, `internal/store/{mysql,postgres,sqlite,mssql}/store.go` y sus dialectos.

### Tareas

- [x] Implementar clasificación de campos y expresiones de proyección limitada para los cinco motores SQL, sin fallback silencioso a valores completos.
- [x] Aplicar selección de columnas y filtros parametrizados en servidor, preservando nombres citados y tipos de parámetros.
- [x] Recuperar claves internas sin mostrarlas si el usuario no las seleccionó; soportar identidad compuesta y únicos no nulos.
- [x] Mantener keyset cuando hay PK simple; ordenar establemente por PK compuesta o índice único completo sin NULLs y usar offset en los demás casos.
- [x] Eliminar el conteo exacto obligatorio por página; usar lookahead para avanzar con total desconocido, incluso cuando no haya PK simple.
- [x] Tratar la fila extra, páginas vacías/finales, columnas ocultas, valores con el texto de un placeholder y metadata fallida.
- [x] Comprobar límites reales de prefijos con UTF-8, JSON, blobs y tipado dinámico de SQLite.
- [x] Sustituir la página activa sin conservar buffers anteriores; verificar que clones/operaciones en curso no acumulen páginas pesadas.
- [x] Crear fixtures sintéticas desechables por dialecto y ejecutar el camino real de `Browse` midiendo bytes retenidos y payload devuelto por la proyección.
- [x] Documentar por motor representación, clasificación y límites del prefijo; no afirmar que obtener longitudes o prefijos evita todo I/O del servidor.

### Evidencia E3 — 2026-10-15

`RelationalAdapter.Browse` usa ahora proyecciones explícitas y de lectura para
los cinco motores SQL del alcance. Pide `PageSize+1`, conserva sólo la página
solicitada, devuelve total desconocido y limpia filas/nulls/referencias/metadata
de la fila lookahead. No ejecuta `COUNT(*)`. La selección de columnas y filtros
AND tipados se aplican en el servidor; los identificadores provienen del
metadata y los valores van parametrizados.

**Identidad y páginas.** `RowReference` mantiene las claves nativas tipadas,
incluidas las compuestas, aunque queden ocultas de `Rows`; claves binarias se
codifican sin ambigüedad sólo en el cursor opaco y vuelven al tipo binario al
consultar la página siguiente. PK simple conserva keyset; PK compuesta e índice
único completo no nulo usan orden estable y offset. Índices parciales/filtrados
no califican. No se usa una preview ni el texto visible para reconstruir claves.
El test SQLite `TestSQLiteBrowseKeysetUsesBinaryIdentityBytes` prueba páginas
con PK binaria; las integraciones comprueban `id=2` como RowRef oculto cuando
la única columna visible es `name`.

**Proyección por motor.** SQLite inspecciona `typeof()` por valor y acota texto
dinámico, JSON declarado/textual y BLOB; distingue TEXT/BLOB, NULL, vacío y
números. MySQL/MariaDB convierte texto/JSON a `utf8mb4` y acota prefijos por
bytes; PostgreSQL limita texto/JSON/JSONB/XML/domains/arrays convertidos a texto
y acota `bytea`; SQL Server limita texto UTF-8/Unicode, JSON guardado como
`NVARCHAR(MAX)`, XML y binarios. Los motores conocidos mantienen valores
escalares; MySQL/SQL Server rechazan metadata cuyo tipo base no pueden clasificar
sin garantizar una salida acotada. Binarios incompletos se representan en la
grilla como prefijo hexadecimal; `Incomplete` registra estado/tipo y `Nulls`
sigue siendo fuente de verdad para NULL. El scanner limita texto retenido a
`preview_bytes` sin cortar una secuencia UTF-8 y ajusta el flag si el driver
devuelve más bytes que ese límite.

**Fixtures y resultados.** Las pruebas `TestIntegrationMySQL`,
`TestIntegrationMariaDB`, PostgreSQL y SQL Server ejecutaron el `Browse` real
con dos filas y columnas de texto, JSON/JSONB (JSON en `NVARCHAR(MAX)` para
SQL Server), XML (`xml`/SQL Server) y blobs binarios de 1 MiB por valor. La
selección `name` con filtro `id > 1` devolvió sólo `Bob` (3 bytes retenidos);
verificó RowRef oculto `id=2`. El Browse que recupera todas las columnas, con
`PreviewBytes: 8`, comprobó UTF-8 (`界xxxxx`), previews JSON/texto/XML y binario (`0x` más 8 bytes).
SQLite añade pruebas de 1 MiB para BLOB y JSON-texto, frontera UTF-8,
tipado dinámico, placeholder literal, lookahead/página final, y metadatos de
incompletitud. No se midieron bytes del protocolo/red: la medición de 3 bytes
es el resultado retenido tras la proyección, no una medición de transferencia.

`Browser.Clone` comparte el puntero inmutable de la página publicada; la prueba
`TestBrowser_CloneNavigationDoesNotMovePublishedPage` verifica que navegar la
copia reemplaza su página y deja intacta la vista original. La TUI serializa
una navegación activa, cancela con contexto/timeout y descarta mensajes obsoletos
(`TestModel_EscCancelsNavigation`). La página previa puede permanecer viva
mientras exista una operación/copia que la referencia; no hay caché de páginas.
Un driver que tarde en atender la cancelación puede mantener recursos hasta que
su llamada regrese; no se promete límite de RSS ni interrupción física inmediata.

**Verificaciones.** `go test ./internal/store/mysql ./internal/store/postgres
./internal/store/mssql -count=1 -v` aprobó contra instancias desechables MySQL,
MariaDB, PostgreSQL y SQL Server; MariaDB se ejecutó por separado en puerto
3308. `go test ./...` aprobó (18 paquetes con pruebas, 4 sin pruebas),
`go build -o /tmp/picklock-e3-check ./cmd/picklock`, `go vet ./...` y
`gofmt -l` de todos los archivos Go tocados no produjeron errores/salida.
Contenedores y fixtures temporales fueron retirados.

**Smoke real de TUI.** Se lanzó ese binario en PTY de 120×32,
`--read-only`, con configuración XDG aislada y SQLite desechable de dos filas;
cada fila tenía TEXT/JSON/BLOB de 1 MiB. La grilla mostró `<preview · TEXT>`,
prefijos `界…`/JSON y el aviso `INCOMPLETE`; el footer indicó `1-2/?`.
Enviar `q` terminó con código 0. El smoke no demuestra transferencia física
limitada ni abre el binario oculto por el scroll horizontal.

**Límites y alcance.** El tipo JSON nativo no existe en SQL Server; se probó
su representación habitual como `NVARCHAR(MAX)` junto con XML y VARBINARY.
Algunos dialectos pueden leer/convertir internamente el valor completo para
producir un prefijo; no se midió I/O del servidor ni tráfico de red. No se
dispuso del contenedor representativo `product_images`; E3 queda cerrada por
sus criterios de servicios sintéticos, y esa aceptación representativa sigue
pendiente de E7. La UI de aviso/formulario de opciones pertenece a E4 y no se
implementó en esta sesión.


### Cierre

- La navegación por defecto limita payloads grandes a prefijos anotados; no devuelve su valor completo.
- Filtrar/seleccionar columnas cambia el conjunto en DB, no sólo su visualización local.
- Se puede avanzar/retroceder sin pérdida de límites por columnas ocultas; no se necesita conocer el total exacto.
- MySQL y MariaDB se verifican por separado aunque compartan implementación.
- Browse real se ejercitó en los cinco motores; no se afirma que los prefijos eviten todo I/O interno del servidor.

## E4 — Apertura y opciones de exploración

- **Estado: COMPLETADA.** Dependencia: E3.

Archivos principales: `internal/tui/{model,session,browser_run,update,keys,mouse}.go`, `internal/tui/screens/`, `internal/browser/browser.go`.

### Tareas

- [x] Implementar estados de preparación, aviso, edición de opciones y carga; el hilo de render no ejecuta metadata/fetch síncronos.
- [x] Aplicar el flujo también a la selección inicial al conectar, refresh y reload tras cambios de estructura.
- [x] Mostrar el aviso por tabla/conexión con `Explorar`, `Acotar` y `Cancelar`; sin inventar totales ni tamaños.
- [x] Crear el formulario de columnas, filtros estructurados y tamaño de página con validación y controles de teclado/ratón.
- [x] Añadir acción de opciones de exploración a keymap, ayuda y footer, comprobando conflictos con atajos actuales.
- [x] Aplicar cambios sólo al confirmar; cancelar no cambia la tabla/página anterior ni marca el aviso como aceptado.
- [x] Mostrar previews/diferidos, totales desconocidos, carga, timeout y cancelación sin sustituir datos reales por strings de UI.
- [x] Descartar mensajes de preparaciones o páginas canceladas; limpiar opciones/avisos de la conexión al desconectar.
- [x] Verificar visualmente en una TUI real las rutas con/sin campos pesados, cancelación, redimensionado y reapertura de opciones.

### Cierre

- Ningún fetch de contenido antecede al aviso cuando éste corresponde.
- El usuario puede recorrer todo o aplicar un subconjunto antes de cargar.
- Cancelar, fallar o recibir una respuesta antigua no cambia la vista vigente.
- Quedan documentados los atajos y el significado de tamaño de página frente a total de tabla.

## E5 — Visor bajo demanda, copia y exportación

**Estado: FINALIZADA.** Dependencia: E4.

Archivos principales: drivers SQL, `internal/tui/detail.go`, ciclo de sesión/update, `internal/tui/export.go`, `internal/export/`.

### Tareas

- [x] Implementar lectura fragmentada en servidor por referencia de fila y columna validada para todos los motores SQL del alcance.
- [x] Establecer lectura consistente sin cambios administrativos; controlar fila eliminada, clave cambiada, valor `NULL`, permisos e indisponibilidad de snapshot.
- [x] Materializar el valor a temporal privado con contexto y progreso, sin asignar el valor entero ni convertir todo el binario a hexadecimal.
- [x] Implementar visor por bloques, navegación textual/binaria y guardado completo; preservar codificación y bytes originales según el contrato.
- [x] Cancelar al cerrar/cambiar de tabla/desconectar; descartar bloques tardíos y cerrar transacciones, cursores y archivos en todos los caminos.
- [x] Aplicar las reglas de copia de preview, clipboard completo acotado y exportación consistente de la página; nunca serializar placeholders como datos.
- [x] Advertir y deshabilitar recuperación puntual ambigua cuando no exista clave fiable, sin impedir la exploración de la tabla.
- [ ] Ampliar pruebas específicas de claves compuestas, cambios concurrentes, errores DB y fallos de escritura local; en esta etapa se validaron bloques UTF-8/binarios, NULL, cancelación y lectura real con clave simple.
- [x] Observar el visor y su cancelación en la TUI real; comprobar el archivo exportado contra el valor sintético esperado mediante longitud y checksum.
- [ ] Medir heap vivo y ciclos repetidos de apertura/cierre/cambio de campo; en esta etapa se midió RSS del proceso durante spool/cierre y se documenta abajo.

### Evidencia final E5 — 2026-10-15

**Código y decisiones.** `ReadCell` está implementado en SQLite, PostgreSQL,
MySQL/MariaDB y SQL Server. Las consultas validan tabla/columna/identidad y
leen bloques dentro de una transacción repeatable-read/snapshot. SQL Server
necesita `ALLOW_SNAPSHOT_ISOLATION ON`; no se activa administrativamente desde
Picklock. Su driver no admite `sql.TxOptions.ReadOnly`, así que la transacción
usa nivel SNAPSHOT sin esa opción; la consulta sigue siendo sólo lectura.
La conversión de texto SQL Server pasa por `NVARCHAR`, aplica collation UTF-8
y luego convierte a `VARCHAR`, evitando que un `界` se degrade a `?`.

`store.SpoolCellProgress` escribe a temporal privado con buffer fijo, contexto
cancelable y bytes/total. El detalle usa un worker asíncrono; al terminar lee
ventanas del archivo. `s` abre ruta destino y `io.Copy` copia desde
`io.NewSectionReader`, sin materializar el valor completo. `c`/`C` copian bytes
spooled sólo hasta `EditorMaxCellBytes`; por encima muestran tamaño/límite y
recomiendan `s`. `a` no copia una fila con otras celdas incompletas.
Exportar una página que tenga preview/diferido se rechaza con explicación
visible; no se serializan placeholders ni se omiten valores. El exportador
existente continúa limitado a resultados/páginas ya seleccionados.

Una primera smoke detectó que `Ctrl+C`/`q` global salían antes de limpiar el
temporal abierto; se corrigió el cierre de `detailCellFile` y se agregó
`TestQuitRemovesSpooledCellFile`. La salida abrupta con `SIGKILL` sigue sin
garantía de limpieza del temporal, como es normal para archivos temporales.

**Servicios reales.** Se usaron contenedores desechables `postgres:16`,
`mysql:8.4`, `mariadb:11.8` y `mcr.microsoft.com/mssql/server:2022-latest`,
con bases `test`; SQL Server tuvo SNAPSHOT habilitado en la base de prueba.
MariaDB estuvo disponible por su IP de bridge Docker (`172.17.0.5`) porque
el driver de integración fija el puerto 3306. Los tests crean/eliminan sus
fixtures `picklock_test` y no tocaron bases preexistentes.

Comando final, ejecutado sin caché:

```sh
PICKLOCK_TEST_POSTGRES_HOST=127.0.0.1 PICKLOCK_TEST_POSTGRES_USER="$PG_USER" PICKLOCK_TEST_POSTGRES_PASSWORD="$PG_PASSWORD" PICKLOCK_TEST_POSTGRES_DATABASE=test \
PICKLOCK_TEST_MYSQL_HOST=127.0.0.1 PICKLOCK_TEST_MYSQL_USER="$MYSQL_USER" PICKLOCK_TEST_MYSQL_PASSWORD="$MYSQL_PASSWORD" PICKLOCK_TEST_MYSQL_DATABASE=test \
PICKLOCK_TEST_MARIADB_HOST=172.17.0.5 PICKLOCK_TEST_MARIADB_USER="$MARIA_USER" PICKLOCK_TEST_MARIADB_PASSWORD="$MARIA_PASSWORD" PICKLOCK_TEST_MARIADB_DATABASE=test \
PICKLOCK_TEST_MSSQL_HOST=127.0.0.1 PICKLOCK_TEST_MSSQL_USER="$MSSQL_USER" PICKLOCK_TEST_MSSQL_PASSWORD="$MSSQL_PASSWORD" PICKLOCK_TEST_MSSQL_DATABASE=test \
go test -count=1 -v ./internal/store/postgres ./internal/store/mysql ./internal/store/mssql
```

**Resultado:** PostgreSQL 0,555 s; MySQL 0,64 s; MariaDB 0,57 s;
SQL Server 1,25 s. Pasaron las lecturas completas UTF-8 de 1.048.579 bytes y
binarias de 1.048.576 bytes, además de las pruebas Browse, TLS y sólo lectura
de esos paquetes. Los tests de integración usan bloques de 64 KiB: mantener
bloques de 3/7 bytes para payloads de 1 MiB causó más de 400 s en PostgreSQL
y el timeout de 10 min en SQL Server por cientos de miles de round-trips.
SQLite conserva las pruebas específicas de bloques pequeños que cruzan UTF-8.
El primer SQL Server run reveló tanto la opción ReadOnly no soportada como la
conversión Unicode defectuosa; tras las correcciones el paquete y la matriz
completa pasaron sin caché.

**Smoke PTY SQLite.** `go run ./cmd/picklock/ --read-only <fixture>` con
`TERM=xterm-256color`, 120×32 y configuración XDG aislada. Fixture desechable
con celdas TEXT de 1.179.648, 2.097.171, 12.582.912 y 33.554.432 bytes.
La interacción real abrió `v`, seleccionó campo, inició `e` y mostró bytes
recibidos y porcentaje (p. ej. 0–65% en muestras de un segundo sobre 32 MiB).
Tras completar 12 MiB, `→/→/←` mostró offsets 4.096/8.192/4.096. `Esc`
canceló spool activo; el temporal desapareció antes de salir, y no quedó
ningún `picklock-cell-*` después de cancelar/salir. Tras `Ctrl+C`, el proceso
terminó con código 0 y sin FD vivos; la prueba de regresión comprueba que el
archivo temporal se elimina al procesar quit.

Guardado desde el visor (`s`) de 2.097.171 bytes: la longitud guardada coincidió
y SHA-256 fue
`13a71903bbb35ec589940c307f6b6531dda57b602e02fee73e8fe5f6610b202e`,
igual al valor sintético. Pruebas TUI también verifican copia por debajo del
límite, rechazo por encima con consejo de guardar a archivo y rechazo explícito
de exportación de páginas con diferidos/previews.

**Memoria y límites medidos.** Con binario construido temporalmente, una celda
SQLite de 33.554.432 bytes y muestreo de `/proc/<pid>/status`: RSS/HWM inicial
21.736 kB; durante spool, RSS 50.632 kB y HWM 58.556 kB; tras cerrar el visor,
RSS 26.500 kB. Esto es RSS (no heap Go), incluye runtime/driver/SQLite y la
muestra fue durante spool activo; no prueba RSS constante ni que el driver no
asigne buffers temporales. No se instrumentó heap vivo ni ciclo repetido de
aperturas. Tampoco se agregó fixture live de clave compuesta/cambio concurrente
ni fallo local forzado; esos casos permanecen explícitos en la lista de tareas.
No se usó la fixture representativa `product_images`.

**Verificación local:** `go test ./internal/...` aprobó 18 paquetes y uno sin
tests; `go build ./cmd/picklock/` aprobó. Los contenedores SQL se detuvieron y
no quedaron archivos `picklock-cell-*` en `/tmp`.

### Cierre

- [x] Se puede recorrer y guardar el valor completo sin bloquear la UI y sin conservarlo entero en heap.
- [x] El resultado corresponde a una fila identificada y una lectura consistente, o informa por qué no puede recuperarse.
- [x] El archivo completo coincide con el contenido esperado; los parciales nunca se presentan como completos.
- [x] No quedan temporales ni operaciones vivas después de cancelación/cierre normal; documentar el comportamiento ante terminación abrupta sin prometer limpieza imposible tras `SIGKILL`.

## E6 — Protección del editor SQL

**Estado: FINALIZADA.** Dependencia: E5.

Archivos principales: `internal/editor/editor.go`, `internal/store/scan.go`, ejecutores de cada motor, `internal/tui/editor_run.go`, render/export de resultados.

### Tareas

- [x] Aplicar el contrato de límites a todos los ejecutores, sin cambiar la sentencia del usuario ni reejecutarla automáticamente.
- [x] Acotar por celda antes de conversiones evitables y detener retención al alcanzar filas, celda o bytes acumulados.
- [x] Tratar coherentemente resultados tabulares, documentos, clave-valor, grafo y texto; los documentos JSON que exceden el presupuesto se omiten completos.
- [x] Propagar motivo, ítems y bytes retenidos; conservar errores DB/cancelación sin esconderlos bajo el aviso de límite.
- [x] Paginar visualmente el resultado retenido, mantener copia/exportación incompleta explícita y orientar a filtrar/seleccionar menos.
- [x] Cubrir celda grande, muchas filas, corte dentro de la primera fila, límite exacto, binario, multibyte y consultas de escritura.
- [x] Ejecutar consultas sintéticas excedidas y observar el aviso/prefijo en la TUI SQLite.
- [x] Medir asignaciones del driver y coste de cierre al superar límites, por motor.
- [x] Documentar límites de Picklock y garantías no ofrecidas antes del fetch del driver; no presentar el presupuesto como RSS duro.

### Evidencia E6 — 2026-10-15

- `internal/store/scan_test.go`: regresiones para preview UTF-8, presupuesto acumulado cortado dentro de la primera fila, tope de filas con registros pequeños, presupuesto exacto y expansión binaria a hex. `internal/editor/editor_test.go`: corte de 3 filas con motivo/bytes exactos y consulta multisentencia conservada sin mutación, con celda multibyte incompleta.
- Smoke PTY SQLite 120×30 (evidencia anterior): `printf('%070000d',1)` produjo `cell_bytes`, 1 ítem y 65.536 B retenidos de 70.000 B; CTE de 10.001 filas produjo `rows`, 10.000 ítems/38.894 B. El aviso recomienda filtrar/seleccionar menos y la vista identifica el prefijo incompleto.
- Benchmark reproducible de integración `internal/store/e6_driver_benchmark_test.go`, fixture de 1 MiB por motor, presupuesto de 64 KiB y resultado comprobado como incompleto con bytes/ítems retenidos exactos. Comando: `go test ./internal/store -run '^$' -bench '^BenchmarkEditorDriverBudget$' -benchmem -benchtime=3x -count=3`, con variables `PICKLOCK_TEST_*` configuradas para cada servicio. Setup y limpieza se excluyen del timer; cada operación medida es `QueryExecutor.Execute` hasta después del cierre normal del cursor/iterador. Linux/amd64, Go `go1.27.2-X:nodwarf5`, Intel Core i7-10510U; imágenes: SQLite embebido, `postgres:16`, `mysql:8.4`, `mariadb:11.8`, `mcr.microsoft.com/mssql/server:2022-latest`, `mongo:7`, `redis:7-alpine`, `cassandra:4.1`, `neo4j:5-community`. Las cifras son medianas de tres invocaciones de tres operaciones:

| Motor | Payload fuente | Bytes/ítems retenidos | B/op | allocs/op | `Execute` ms/op |
|---|---:|---:|---:|---:|---:|
| SQLite | 1 MiB | 65.536 B / 1 | 2.296.317 | 40 | 1,49 |
| PostgreSQL | 1 MiB | 65.536 B / 1 | 1.753.066 | 46 | 4,27 |
| MySQL | 1 MiB | 65.536 B / 1 | 2.173.488 | 42 | 10,41 |
| MariaDB | 1 MiB | 65.536 B / 1 | 2.173.541 | 42 | 9,45 |
| SQL Server | 1 MiB | 65.536 B / 1 | 4.207.368 | 582 | 18,25 |
| MongoDB | 1 MiB | 0 B / 0 | 5.316.746 | 126 | 4,20 |
| Redis | 1 MiB | 65.536 B / 1 | 1.058.861 | 31 | 0,78 |
| Cassandra | 1 MiB | 65.536 B / 1 | 2.117.941 | 116 | 7,12 |
| Neo4j | 1 MiB | 65.536 B / 1 | 9.687.976 | 520 | 23,85 |

- `B/op` incluye executor, driver, fetch/decodificación y conversiones; no es una atribución aislada al driver ni RSS. Un perfil `alloc_space` del proceso mostró stacks de drivers como `modernc.org/sqlite.columnBlob`, `pgx` scan, `go-sql-driver/mysql`, `go-mssqldb.readPLPType`, decodificación BSON, lector RESP, `gocql` frame/`unmarshalVarchar` y Neo4j Bolt; el perfil también incluye setup/limpieza de fixtures y no permite atribuir bytes por driver. El procesamiento del editor también contribuye; Neo4j además tokeniza el literal de 1 MiB de la consulta.
- El tiempo `Execute` termina después del cierre normal: incluye fetch, conversión y cierre, pero no separa cuánto corresponde al `Close` en cada motor. SQLite sí tiene medición separada de `Rows.Close` en `BenchmarkScanResultLimitsDriverSQLite`: para 128 KiB/1 MiB/8 MiB de origen, el prefijo retenido fue siempre 65.536 B y las medianas de cierre 40.608/177.632/1.444.156 ns/op. Redis no mantiene cursor por comando: `GET` materializa el resultado antes del recorte. Los ejecutores MongoDB, Cassandra y relacionales cierran cursor/iterador al cortar; Neo4j consume/cierra el resultado después del corte. Los errores de close/error de resultado se mantienen según el contrato de cada executor; el benchmark falla ante error o falta de metadata de corte.
- Límites no atribuibles a RSS: MongoDB decodifica BSON y serializa JSON antes de decidir omitir el documento; Redis recibe el valor completo de `GET`; los resultados relacionales, Cassandra y Neo4j se asignan/decodifican antes del truncado en Go. MongoDB conserva cero contenido cuando un documento completo excede el presupuesto. El presupuesto limita contenido retenido, no buffers del driver, transferencia, JSON temporal, memoria del servidor ni RSS.
- Suites live de PostgreSQL, MySQL, MariaDB, SQL Server, MongoDB, Redis, Cassandra y Neo4j aprobaron con contenedores desechables; además `go test -count=1 ./internal/...` con los ocho servicios configurados reportó 18 paquetes aprobados y uno sin tests. El test Redis quedó actualizado: `HGETALL` con máximo 10 ahora espera 10 entradas y `LimitRows` explícito, no las 250 completas.
- Se conservan la sentencia elegida y sus efectos; no se agregan `LIMIT`/wrappers a SQL arbitrario ni se reejecuta. Cortes/errores no se presentan como completos. Las medidas no cubren el fixture real `product_images` ni garantizan que el servidor deje de leer/calcular un valor completo.

### Cierre

- [x] Los límites de filas, celda y acumulado se ejercitan y producen información correcta para el usuario en los formatos soportados.
- [x] Render, copia y exportación conservan la marca de incompletitud.
- [x] Se conserva la semántica de la sentencia; no se agregan `LIMIT` o envoltorios a consultas arbitrarias.
- [x] Se registran asignaciones, coste end-to-end con cierre, cierres observables y limitaciones de transferencia por driver.

## E7 — Validación integral y cierre

**Estado: ACTIVA.** Dependencia: E6.

### Fixtures y entorno

- [ ] Preparar scripts SQL reproducibles por motor en `internal/store/testdata/heavy-columns/`, con ejecución explícita contra servicios/bases desechables, nunca auto-seed de una base del usuario.
- [ ] Incluir texto/Base64, binarios y JSON con tamaños variables y valores límite; datasets de 10, 100, 1.000 y 10.000 filas.
- [ ] Incluir PK simple/compuesta, único no nulo, ausencia de identidad fiable, identificadores citados, vacíos/NULL y payload mayor que el presupuesto del editor.
- [ ] Preparar/adaptar el contenedor MySQL del caso `product_images`: misma versión concreta, esquema, charset e índices; aproximadamente 74 filas con distribución de tamaños representativa, incluido el mayor.
- [ ] Separar credenciales de preparación de la fixture y usuario de validación de sólo lectura. No guardar dumps, imágenes reales ni secretos en el repositorio.

### Mediciones y escenarios

- [ ] Registrar versiones, equipo, configuración, filas y tamaños; separar primera ejecución y repeticiones, distinguiendo cachés controladas de las no controladas.
- [ ] Medir por separado preparación/metadata, fetch/proyección, conversión y render; además tiempo desde la acción hasta página utilizable.
- [ ] Medir RSS, heap vivo y asignaciones acumuladas como magnitudes distintas. El heap/RSS no tienen por qué bajar inmediatamente al cerrar: comprobar retención y tendencia tras ciclos repetidos.
- [ ] Medir latencia del primer bloque y tiempo total de materialización/guardado, con tamaño/checksum del resultado.
- [ ] Navegar repetidamente páginas/tablas; abrir/cerrar/cancelar detalles, cambiar de tabla durante fetch y desconectar durante lectura.
- [ ] Repetir apertura de detalle para comprobar que sólo queda vigente la última operación, no acumulación de trabajos/temporales.
- [ ] Verificar que la UI procesa entrada, repinta progreso y responde a cancelación mientras hay DB/I/O pendiente. Distinguir latencia local de UI de cuánto tarda el servidor en cancelar.
- [ ] Ejecutar las pruebas pertinentes y, al estabilizar la entrega integrada, `go test ./internal/...` y `go build ./cmd/picklock/` una vez. Registrar comandos y salida real.
- [ ] Lanzar la aplicación real con `go run ./cmd/picklock/` y completar la matriz visual/funcional; los tests de modelo no sustituyen esta prueba.

### Aceptación final

- [ ] `product_images` se explora sin congelar la TUI y sin recuperar por defecto todo `image_data`.
- [ ] Aumentar el tamaño de los campos diferidos no hace crecer proporcionalmente el contenido retenido de la página. No exigir RSS constante ni atribuir al cliente el I/O que aún realice el servidor.
- [ ] Abrir un valor completo permite navegar/guardar contenido correcto con UI responsiva, cancelación y recursos liberados.
- [ ] Columnas/filtros/página elegidos por el usuario se aplican antes del fetch; explorar sin filtros sigue permitiendo recorrer toda la tabla.
- [ ] Los cinco motores SQL tienen evidencia real de proyección/lectura; los demás motores no pierden su navegación ni la semántica de sus consultas.
- [ ] Actualizar README/USAGE y documentos de contratos/limitaciones afectados, con ejemplos, atajos, defaults y restricciones observadas.
- [ ] Registrar la evidencia final en la bitácora y preparar el informe para el issue. No publicar ni cerrar GitHub automáticamente sin una instrucción para ello.

Condición de entrega: no quedan scaffolds ni programas temporales de medición; las fixtures y los benchmarks reproducibles sí se conservan.

## Estado de validación y recursos externos

| Recurso/comprobación | Estado actual | Consecuencia |
|---|---|---|
| Lectura del issue y del código relevante | REALIZADA | Sustenta E0 y este plan |
| Smoke sintético del render actual | REALIZADA | Baseline E0; no prueba la solución |
| Implementación E1–E7 | E1–E6 FINALIZADAS; E7 ACTIVA | E6 completada tras smoke SQLite y medición live de los nueve motores; E7 retiene la aceptación integral y el caso `product_images` |
| Contenedor `multitech-mysql-local` con `ecommerce.product_images` | ENTREGADO; backend validado parcialmente | MySQL 8.0.46, InnoDB, `utf8mb4_0900_ai_ci`, 212 filas, PK `id`, índice `id_product`; `image_data` LONGTEXT, tamaño máximo 3.383.488 B. No coincide con el tamaño de referencia aproximado de 74 filas; falta validar UI/latencia y obtener credenciales separadas de sólo lectura. |
| Servicios de integración E5/E6 | VERIFICADOS EN VIVO; CONTENEDORES DETENIDOS | PostgreSQL, MySQL, MariaDB, SQL Server, MongoDB, Redis, Cassandra y Neo4j; mediciones detalladas en evidencia E6 |
| Smoke PTY editor E6 | REALIZADO EN SQLITE | Celda excedida y tope de filas observados; no representa `product_images` |
| Benchmark E6 SQLite | REALIZADO | 128 KiB/1 MiB/8 MiB de origen frente a 64 KiB retenidos; `Rows.Close` medido separadamente |
| Benchmark E6 servicios | REALIZADO | Nueve motores, payload sintético de 1 MiB y budget 64 KiB; asignaciones end-to-end y `Execute` con cierre incluido |

La validación backend real no sustituye el smoke visual/funcional de TUI ni acredita latencia, cancelación o memoria/RSS. La tabla entregada contiene 212 filas; conservar la diferencia respecto a la referencia aproximada de 74 y documentar el dataset efectivamente ejercitado.

## Bitácora de iteraciones

| Fecha | Etapa | Cambio/evidencia | Siguiente acción |
|---|---|---|---|
| 2026-10-15 | E0 FINALIZADA; E1 ACTIVA | Diagnóstico contrastado con código, smoke sintético y documentación de clientes; plan detallado creado. Sin implementación funcional. | Corregir truncado y cálculo de anchos; añadir regresiones/benchmark y observar la TUI. |
| 2026-10-15 | E1 ACTIVA — implementación | Recorte incremental por grafemas con `uax29` ya presente y medición `runewidth`; anchos cortan al llegar a 36 y omiten celdas posteriores de columnas saturadas. LSP confirmó que `colWidths` y `renderHeader` no tenían consumidores productivos: eliminados junto con pruebas del helper muerto. Revisados resultados del editor y detalle; el detalle envuelve sólo hasta el final del viewport, sin introducir visor/lectura DB. Regresiones y benchmark añadidos; verificaciones en curso, aún sin cierre. | Ejecutar pruebas, smoke sintético y navegación/redimensionado de la TUI con fixture desechable; registrar resultados. |
| 2026-10-15 | E1 FINALIZADA; E2 ACTIVA | Nueve tareas E1 completadas. `go test ./internal/...`, benchmark ×3, build y `go vet ./...` aprobados; smoke sintético 1 KiB–16 MiB con salida idéntica y TUI real con 74 celdas de 1.048.584 B: páginas 50/24, selección, NULL/vacío, scroll, resize, detalle, editor y salida 0. Evidencia/mediciones y límites arriba; docs y atajos actualizados; revisión arquitectónica sin bloqueantes. Temporales retirados. Sin cambios de transferencia DB ni publicación en GitHub. | Iniciar contratos y preferencias de E2 en la próxima sesión; no comenzar E3. |
| 2026-10-15 | E2 ACTIVA — implementación | Contrastados contratos actuales: `BrowseRequest` sólo paginaba, `QueryExecutor.Execute` recibía `maxRows`, preferencias sólo timeout/layout y `Browser.New` inspeccionaba/cargaba automáticamente. Se introducen contratos tipados de opciones/filtros/identidad/incompletitud/lectura opcional, migración de presupuestos y formulario de preferencias. Preparación separada de carga; metadata fallida no habilita fetch. Decisión de alcance: E2 valida y transporta presupuestos; límites de bytes efectivos son E6, proyección/filtros SQL son E3 y lectura completa E5. Hasta entonces opciones no implementadas se rechazan explícitamente. | Integrar consumidores, ejecutar pruebas y smoke de ajustes/paginación SQLite; registrar resultados antes de cerrar. |
| 2026-10-15 | E2 FINALIZADA; E3 ACTIVA | Ocho tareas E2 completas: contratos tipados, presupuestos migrados en todos los ejecutores/callers incluido MCP, incompletitud segura, preparación separada y siete ajustes validados/persistidos. Suite completa/build/vet aprobados; smoke SQLite y TUI real con archivo viejo, guardado/reinicio, rechazo de inválidos, páginas y exportación parcial sin archivo. Smoke detectó aviso oculto en panel corto: corregido, regresión y smoke final aprobados. Evidencia y límites arriba; temporales retirados. No se implementó E3 ni se publicó en GitHub. | Iniciar E3: proyecciones/clasificación/filtros reales, claves internas y lookahead; comprobar servicios por motor antes de declarar cobertura. |
| 2026-10-15 | E3 ACTIVA — implementación parcial | Eliminado `COUNT(*)`; lookahead, total desconocido, orden estable por PK simple/compuesta e índices únicos completos no nulos; metadatos de índices parciales; proyección explícita/filtros parametrizados al solicitar opciones en cuatro drivers. SQLite cubierto por regresiones/smokes; suites de integración MySQL 8.4.11, MariaDB 11.8.8, PostgreSQL 16.14 y SQL Server 2022 CU26 aprobaron con Browse real (`name`, `id > 1`, resultado Bob); MariaDB en puerto 3308 tras conflicto en 3307. `go test ./...`, build y vet aprobados. `SELECT *` predeterminado, prefijos seguros, RowRefs y medición de transferencia siguen pendientes; E3 no se cerró. | Implementar clasificación y proyecciones acotadas; agregar RowRefs y fixtures de payload para medir transferencia por dialecto. |
| 2026-10-15 | E3 FINALIZADA; E4 ACTIVA | Clasificación/proyección acotada, RowRefs ocultos y cursores tipados en SQLite/MySQL/MariaDB/PostgreSQL/SQL Server; fixtures ejercitan texto UTF-8, JSON, XML donde aplica, blobs y dinámico SQLite. Integraciones Browse real aprobaron con `name`, filtro `id > 1`, 3 bytes retenidos y `id=2` oculto; `PreviewBytes:8` validó previews. `go test ./...` (18 paquetes), build y `go vet ./...` aprobaron; smoke TUI SQLite real 120×32 mostró `<preview>`, INCOMPLETE, `1-2/?`, salida 0. Documentación y checklist E3 actualizados; sin medir tráfico de red ni usar fixture `product_images`. Contenedores/temporales retirados. Sin publicar/cerrar GitHub. | Empezar E4 con preparación/aviso/formulario y smoke de TUI; no comenzar E5. |
| 2026-10-15 | E4 FINALIZADA; E5 ACTIVA — primera iteración | E4 completada y verificada; evidencia en la sección E4. En E5, el adapter publica `ReadCell` por delegación y SQLite lee bloques binarios bajo transacción/snapshot con RowRef/columna validados; `SpoolCell` usa buffer fijo y temporal privado, limpia errores y deja el archivo rebobinado. `go test ./internal/store/...` aprobó antes del caso NULL; tras cubrir NULL se ejecutó `go test ./internal/store/sqlite ./internal/store`, aprobado. Prueba real SQLite contrastó bytes exactos de texto UTF-8 y BLOB en bloque de 3 bytes, más NULL. No hay visor/consumidores; los otros motores y la suite integral E5 quedan pendientes. | Implementar lectura consistente fragmentada en MySQL/MariaDB, PostgreSQL y SQL Server y cubrir cancelación/errores del spool. |
| 2026-10-15 | E5 ACTIVA — segunda iteración parcial | Implementados lectores `ReadCell` de PostgreSQL, MySQL/MariaDB y SQL Server; transacciones repeatable-read/snapshot, tamaños y fragmentos server-side, identificadores validados. Agregadas pruebas de integración para texto UTF-8 y bytes binarios por bloques. La TUI inicia spool asíncrono, presenta avance en bytes y porcentaje con tamaño conocido, navega el temporal en bloques y cancela/limpia al salir, cambiar tabla o desconectar. `go test ./internal/store/postgres ./internal/store/mysql ./internal/store/mssql ./internal/store/sqlite ./internal/tui` y `go build ./cmd/picklock` aprobaron; el test de progreso del spool está en `internal/store/cell_spool_test.go`. Diez pruebas de integración se omitieron por falta de servicios SQL configurados; no hubo smoke visual. SQL Server requiere SNAPSHOT habilitado de antemano. Faltan copia/guardado/exportación, escenarios de fallos/cambios concurrentes y mediciones. | Smoke visual de la TUI y ejecución de lectores contra servicios SQL disponibles; completar copia y exportación antes de cerrar E5. |
| 2026-10-15 | E5 FINALIZADA; E6 ACTIVA | Completo visor/spool con guardado streaming (`s`), copia real bajo `EditorMaxCellBytes`, rechazo explícito de exportación de páginas incompletas y limpieza de temporales al Esc/quit/cambio de tabla. Smoke PTY SQLite: progreso bytes/% en payloads hasta 32 MiB, paginación de 12 MiB, cancelación, cambio a tabla `other`; guardado de 2.097.171 B con SHA-256 esperado. Live SQL uncached: PostgreSQL/MySQL/MariaDB/SQL Server PASS (`go test -count=1 -v ...`, 3,1 s conjunto); SQL Server corrigió snapshot ReadOnly no soportado y serialización UTF-8. `go test ./internal/...` (18 paquetes), build y temporales limpios. RSS muestreado (21.736 kB inicial, HWM 58.556 kB); heap vivo y pruebas específicas de composite/concurrencia/errores locales quedan anotados como pendientes no bloqueantes del cierre E5. Contenedores detenidos; sin fixture `product_images` ni publicación GitHub. | Iniciar E6 con límites efectivos del editor SQL; conservar los pendientes anotados para la matriz integral E7.
| 2026-10-15 | E6 ACTIVA — límites implementados | Scan SQL acotado en filas/celda/bytes antes de stringify/hex; los ejecutores Mongo, Neo4j, Redis y Cassandra también publican límites/status y cierran cursores. Regresiones: celda pesada, primera fila, muchas filas, límite exacto, binario/multibyte, status exacto y SQL elegido sin mutación. `go test ./internal/...` (18 paquetes) y build aprobados. Smoke PTY SQLite 120×30: celda 70.000 B → `cell_bytes`, 65.536 B retenidos; CTE 10.001 filas → `rows`, 10.000 ítems/38.894 B; aviso incluye guía para filtrar/seleccionar menos. Mongo decodifica BSON antes de calcular JSON indentado exacto; Redis materializa el resultado antes del recorte. | Medir asignaciones/cierre; iniciar E7 sólo tras completar esa evidencia. |
| 2026-10-15 | E6 FINALIZADA; E7 ACTIVA | Completados límites y pruebas de resultados parciales; benchmark reproducible por los nueve motores midió asignaciones y tiempo end-to-end con setup/teardown excluidos y cierre incluido; `Rows.Close` SQLite también se midió separado. Suites live de ocho motores y `go test -count=1 ./internal/...` aprobaron. Actualizados regresión Redis obsoleta, evidencia y límites BSON/Redis. Sigue sin fixture `product_images`; no se afirma cierre del issue. | Ejecutar E7: fixtures reproducibles por motor, validación integral/visual y caso representativo cuando llegue. |
| 2026-10-15 | E7 ACTIVA — primera iteración | Añadida `internal/store/testdata/heavy-columns/mysql.sql`: datos sintéticos reproducibles para 10/100/1.000/10.000 filas, texto UTF-8/Base64, binario/JSON, límites de payload, NULL/vacío, PK simple/compuesta, índice único, tabla sin identidad y nombres citados. No incluye datos reales ni crea usuarios/permisos. La fixture `product_images` representativa sigue pendiente; el servidor MySQL no estaba activo y no hay cliente `mysql`, así que el script no se ejecutó y no se declara validado. | Validar el script en MySQL 8.4 desechable; agregar fixture por motores y recibir el contenedor/dataset `product_images` con versión, esquema, charset, índices y credenciales separadas. |
| 2026-10-19 | E7 ACTIVA — validación real MySQL | Añadido `TestIntegrationProductImages`, de sólo lectura, y soporte de `_PORT` en `envCfg`; conectado a `multitech-mysql-local` (`127.0.0.1:3308`, base `ecommerce`, MySQL 8.0.46). `go test ./internal/store/mysql -run '^TestIntegrationProductImages$' -count=1 -v` PASS: proyección `id,mime_type` excluye `image_data`; paginación recorrió 212 filas en 5 páginas reteniendo previews de hasta 64 B; `ReadCell` leyó en bloques de 64 KiB el mayor valor, 3.383.488 B. Consulta de metadata identificó 212 filas, máximo 3.383.488 B, total 33.833.294 B. Sin DDL ni modificación de datos. Límite: no se probó TUI, render, cancelación, heap/RSS, checksum del payload ni credencial de sólo lectura; la fixture sintética SQL sigue sin ejecutarse. | Smoke visual/funcional de TUI contra este contenedor con usuario de sólo lectura; medición de respuesta/cancelación y validar fixture SQL en base desechable; continuar fixtures de otros motores. |
| 2026-10-19 | E7 ACTIVA — fixture MySQL validada | Corregido el generador numérico: CTE recursivo en lugar de reinsertar sobre la misma tabla temporal; payloads grandes y límites se reservan a las primeras 10 filas, y el resto de 10/100/1.000/10.000 usa tamaños acotados variables para evitar crecimiento GB. Fixture rerun exit 0 en MySQL 8.0.46 `ecommerce`; conteos confirmados 10, 100, 1.000, 10.000 y 10 por cada tabla de claves compuesta/sin identidad. Dataset permanece en la base desechable para pruebas. | Completar smoke TUI y métricas de respuesta/cancelación; fixture por otros motores. |

En próximas filas registrar: tareas completadas, archivos relevantes, comandos/escenarios ejercitados, resultado observado y limitaciones. Ante fallo o bloqueo, incluir el siguiente paso accionable. No repetir `PASS` sin haber ejecutado la comprobación.

## Referencias

- [Guía de diseño](00-guide.md).
- [Plan histórico de implementación](04-implementation.md).
- [Implementación de motores no relacionales](../non-relational/04-implementation.md).
- [MySQL Workbench: preferencias del editor](https://dev.mysql.com/doc/workbench/en/wb-preferences-sql-editor.html).
- [MySQL Workbench: grilla y editor de valores](https://dev.mysql.com/doc/workbench/en/wb-develop-sql-editor-results.html).
- [DBeaver: preferencias del editor de datos](https://dbeaver.com/docs/dbeaver/Data-Editor-preferences/).
- [DataGrip: límites del editor y visor](https://www.jetbrains.com/help/datagrip/settings-tools-database-data-views.html).

La documentación de clientes confirma controles de visualización, carga y memoria; no demuestra que todos utilicen la misma reescritura SQL o estrategia interna de recuperación diferida.
