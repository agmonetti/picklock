# 08 — Plan iterativo: navegación segura y columnas pesadas

Issue: [#1 — Mejorar rendimiento y estabilidad al explorar tablas con columnas pesadas](https://github.com/agmonetti/picklock/issues/1).

Este documento es el plan vigente para ese issue, no un registro de funcionalidades ya implementadas. Complementa el plan histórico de construcción de Picklock. Para los motores actualmente soportados prevalecen el código y la documentación de `../non-relational/`; este trabajo no elimina motores existentes.

## Seguimiento de ejecución

**Etapa activa: E4 — Apertura y opciones de exploración.**

Actualizado: 2026-10-15. E2 y E3 FINALIZADAS y verificadas; E4 ACTIVA.

| Etapa | Estado | Depende de | Resultado requerido |
|---|---|---|---|
| E0 — Diagnóstico y decisiones | FINALIZADA | — | Problema localizado, evidencia inicial y diseño de referencia |
| E1 — Renderizado con trabajo acotado | FINALIZADA | E0 | Dibujar una celda no recorre repetidamente su contenido completo |
| E2 — Contratos y preferencias | FINALIZADA | E1 | Opciones de exploración, estados de celda, identidad y límites explícitos |
| E3 — Navegación segura en drivers SQL | FINALIZADA | E2 | Proyecciones acotadas y paginación sin descargar payloads completos |
| E4 — Apertura y opciones de exploración | ACTIVA | E3 | Aviso contextual y controles del usuario antes del primer fetch |
| E5 — Visor bajo demanda, copia y exportación | PENDIENTE | E4 | Acceso completo cancelable sin cargar el valor entero en la grilla |
| E6 — Protección del editor SQL | PENDIENTE | E5 | Presupuestos de filas/bytes y resultados incompletos identificables |
| E7 — Validación integral y cierre | PENDIENTE | E6 | Matriz de motores, métricas y aceptación del caso reportado |

### Reglas para cada iteración

1. Mantener exactamente una etapa `ACTIVA` hasta completar el plan. Al cerrar E7, todas deben quedar `FINALIZADA` y la etapa activa será `ninguna`.
2. Trabajar en orden. Una etapa activa indica el foco actual; no significa que sus tareas estén implementadas.
3. Marcar `[x]` sólo después de realizar la tarea. Marcar la etapa `FINALIZADA` sólo cuando se cumplan sus criterios de cierre y exista evidencia registrada.
4. En la misma actualización que cierra una etapa, activar la siguiente y cambiar el encabezado de etapa activa. No reabrir etapas cerradas para esconder regresiones: registrar la corrección y su impacto.
5. Si falta un recurso externo, marcar la etapa `BLOQUEADA`, describir qué falta y conservar un único foco explícito. No presentar una verificación no ejecutada como aprobada ni saltar dependencias.
6. Después de cada iteración, actualizar tareas, estado, bitácora, verificación y siguiente acción. Las decisiones nuevas deben registrar motivo y efecto sobre el alcance.
7. Los cambios de interfaz incluyen todos sus consumidores, pruebas y documentación; no dejar aliases o rutas obsoletas para evitar la migración.
8. Verificar una vez estabilizado cada cambio de etapa, no ejecutar build/tests tras cada edición. El smoke del comportamiento real es obligatorio además de las pruebas pertinentes.

**Siguiente acción concreta:** iniciar E4: implementar aviso y formulario de opciones antes del primer fetch, también en conexión inicial/refresh/reload; preservar la vista ante cancelación, error o respuesta obsoleta. No comenzar E5.

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

**Estado: ACTIVA.** Dependencia: E3.

Archivos principales: `internal/tui/{model,session,browser_run,update,keys,mouse}.go`, `internal/tui/screens/`, `internal/browser/browser.go`.

### Tareas

- [ ] Implementar estados de preparación, aviso, edición de opciones y carga; el hilo de render no ejecuta metadata/fetch síncronos.
- [ ] Aplicar el flujo también a la selección inicial al conectar, refresh y reload tras cambios de estructura.
- [ ] Mostrar el aviso por tabla/conexión con `Explorar`, `Acotar` y `Cancelar`; sin inventar totales ni tamaños.
- [ ] Crear el formulario de columnas, filtros y tamaño de página con validación y controles de teclado/ratón consistentes con los existentes.
- [ ] Añadir acción de opciones de exploración a keymap, ayuda y footer, comprobando conflictos con atajos actuales.
- [ ] Aplicar cambios sólo al confirmar; cancelar no cambia la tabla/página anterior ni marca el aviso como aceptado.
- [ ] Mostrar previews/diferidos, totales desconocidos, carga, timeout y cancelación sin sustituir datos reales por strings de UI.
- [ ] Descartar mensajes de preparaciones o páginas canceladas; limpiar opciones/avisos de la conexión al desconectar.
- [ ] Verificar visualmente en una TUI real las rutas con/sin campos pesados, cancelación, redimensionado y reapertura de opciones.

### Cierre

- Ningún fetch de contenido antecede al aviso cuando éste corresponde.
- El usuario puede recorrer todo o aplicar un subconjunto antes de cargar.
- Cancelar, fallar o recibir una respuesta antigua no cambia la vista vigente.
- Quedan documentados los atajos y el significado de tamaño de página frente a total de tabla.

## E5 — Visor bajo demanda, copia y exportación

**Estado: PENDIENTE.** Dependencia: E4.

Archivos principales: drivers SQL, `internal/tui/detail.go`, ciclo de sesión/update, `internal/tui/export.go`, `internal/export/`.

### Tareas

- [ ] Implementar lectura fragmentada en servidor por referencia de fila y columna validada para todos los motores SQL del alcance.
- [ ] Establecer lectura consistente sin cambios administrativos; controlar fila eliminada, clave cambiada, valor `NULL`, permisos e indisponibilidad de snapshot.
- [ ] Materializar el valor a temporal privado con contexto y progreso, sin asignar el valor entero ni convertir todo el binario a hexadecimal.
- [ ] Implementar visor por bloques, navegación textual/binaria y guardado completo; preservar codificación y bytes originales según el contrato.
- [ ] Cancelar al cerrar/cambiar de tabla/desconectar; descartar bloques tardíos y cerrar transacciones, cursores y archivos en todos los caminos.
- [ ] Aplicar las reglas de copia de preview, clipboard completo acotado y exportación consistente de la página; nunca serializar placeholders como datos.
- [ ] Advertir y deshabilitar recuperación puntual ambigua cuando no exista clave fiable, sin impedir la exploración de la tabla.
- [ ] Probar límites de bloque, claves compuestas/binarias, cambios concurrentes, cancelación durante lectura, error DB y error de escritura local.
- [ ] Observar el visor y su cancelación en la TUI real; comprobar el archivo exportado contra el valor sintético esperado mediante longitud y checksum.
- [ ] Medir heap vivo y RSS al abrir, cerrar y cambiar repetidamente de campo; actualizar documentación de consistencia, disco y privacidad.

### Cierre

- Se puede recorrer y guardar el valor completo sin bloquear la UI y sin conservarlo entero en heap.
- El resultado corresponde a una fila identificada y una lectura consistente, o informa por qué no puede recuperarse.
- El archivo completo coincide con el contenido esperado; los parciales nunca se presentan como completos.
- No quedan temporales ni operaciones vivas después de cancelación/cierre normal; documentar el comportamiento ante terminación abrupta sin prometer limpieza imposible tras `SIGKILL`.

## E6 — Protección del editor SQL

**Estado: PENDIENTE.** Dependencia: E5.

Archivos principales: `internal/editor/editor.go`, `internal/store/scan.go`, ejecutores de cada motor, `internal/tui/editor_run.go`, render/export de resultados.

### Tareas

- [ ] Aplicar el contrato de límites a todos los ejecutores, sin cambiar la sentencia del usuario ni reejecutarla automáticamente.
- [ ] Acotar representación por celda antes de conversiones/codificaciones evitables y detener retención al llegar al presupuesto acumulado o de filas.
- [ ] Tratar coherentemente resultados tabulares, documentos, clave-valor, grafo y texto; no truncar JSON estructurado produciendo un objeto falsamente válido/completo.
- [ ] Propagar motivo del corte, bytes y filas/items retenidos; conservar errores DB/cancelación sin esconderlos bajo el aviso de límite.
- [ ] Paginar visualmente el contenido retenido y reutilizar las reglas de copia/exportación incompleta; explicar cómo refinar la consulta para obtener otro subconjunto.
- [ ] Cubrir una celda grande con pocas filas, muchas filas pequeñas, primera fila que no cabe, límite exacto, binario expandido, multibyte y consultas de escritura existentes.
- [ ] Ejecutar consultas sintéticas reales y observar el resultado/aviso/cancelación en la TUI. Medir asignaciones del driver y coste de cierre cuando excede el presupuesto.
- [ ] Documentar qué limita Picklock y qué no puede garantizar antes del fetch del driver; no describir el presupuesto como un límite duro de RSS.

### Cierre

- Tanto el límite de filas como el de celda y el acumulado se ejercitan y producen información correcta para el usuario.
- El renderizado/copia/exportación no convierten un resultado parcial en uno completo.
- El SQL y los efectos de las operaciones solicitadas conservan su semántica; no se agregan `LIMIT` o envoltorios a sentencias arbitrarias.
- Se registran las limitaciones observadas de transferencia/asignación/cierre por driver, no sólo el tamaño final retenido.

## E7 — Validación integral y cierre

**Estado: PENDIENTE.** Dependencia: E6.

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
| Implementación E1–E7 | E1–E3 REALIZADAS; E4 ACTIVA; E5–E7 SIN IMPLEMENTAR | Proyecciones acotadas, RowRefs, filtros y lookahead ejercitados en SQLite, MySQL, MariaDB, PostgreSQL y SQL Server; visor/presupuestos del editor pendientes |
| Contenedor representativo de `product_images` | PENDIENTE DE ENTREGA | Necesario para aceptación representativa final; no bloqueó el cierre E3 con fixtures sintéticas |
| Servicios SQL de integración | PRUEBAS EJECUTADAS; CONTENEDORES DETENIDOS | MySQL, MariaDB, PostgreSQL y SQL Server aprobaron; MariaDB se aisló en puerto 3308 |
| Smoke del comportamiento corregido | REALIZADO EN SQLITE | TUI read-only real con dos filas y payloads de 1 MiB; mostró previews/INCOMPLETE y total desconocido. No mide transferencia de red |

Si el contenedor representativo no llega, completar todo lo verificable con fixtures sintéticas y registrar el impedimento de aceptación representativa. No declarar resuelto el caso real por haber pasado sólo SQLite o una muestra reducida.

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
