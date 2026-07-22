# Contexto para agentes

Este documento define las reglas de arquitectura y operación del repositorio.

## Stack inmutable

- Backend: Go y la librería estándar `net/http`.
- Base de datos: SQLite mediante `github.com/mattn/go-sqlite3`.
- Interactividad frontend: HTMX.
- Estado de interfaz: Alpine.js.
- Assets y templates: embebidos en el binario con `//go:embed`.

No migrar a PostgreSQL, React, Vue, Next.js ni otro framework SPA.

## Regla HTMX/Alpine

No modificar ningún flujo HTMX existente.

- No renombrar rutas.
- No renombrar IDs utilizados para targeting.
- No modificar atributos `hx-*`.
- No cambiar `hx-trigger`, `hx-swap` o el contenido esperado por los partials.
- No reemplazar respuestas HTML por JSON.
- No cambiar el comportamiento observable.
- Mantener Alpine.js únicamente para estado efímero, observers y validación.

Solo se permite eliminar JavaScript claramente muerto o duplicado, previa verificación de que no se usa.

## Persistencia

- El contenedor debe ser stateless.
- `/data` es el único directorio persistente.
- La base por defecto es `/data/site.db`.
- No guardar bases de datos, uploads ni secretos dentro del código fuente.
- Las migraciones viven en `migrations/`, se embeben y son append-only.
- Nunca editar una migración aplicada; crear otra con un número mayor.

## Configuración

La aplicación usa únicamente estas variables:

- `PORT`
- `DATABASE_PATH`
- `APP_ENV`
- `LOG_LEVEL`

Cada variable debe tener un default razonable. No usar `panic()` ni `log.Fatal()` para configuración normal. Un `DATABASE_PATH` inválido debe producir un error de arranque claro.

## Build y despliegue

- El Dockerfile debe ser multi-stage y usar BuildKit.
- La imagen debe ejecutar como usuario no privilegiado.
- Templates, CSS, JavaScript e imágenes se sirven desde el binario embebido.
- Solo `/data` se monta como volumen en Coolify.
- Coolify debe construir desde el `Dockerfile`, detrás de Traefik.
- No asumir HTTPS en la aplicación ni añadir redirects basados en headers proxy.
- `PORT` es el puerto real de escucha; `EXPOSE 8080` es únicamente metadata Docker.
- Mantener `HEALTHCHECK`, `/health`, `/ready`, `/version` y `/metrics` operativos.

## SQLite

Conservar el driver `github.com/mattn/go-sqlite3` salvo una decisión técnica documentada. Mantener WAL, `synchronous=NORMAL`, foreign keys y busy timeout. Verificar siempre el comportamiento de `database/sql` y del pool antes de cambiar la configuración.

## Cambios frontend

La estética usa `Instrument Serif`, `Space Mono`, bordes duros de 1px y la paleta negro, blanco y azul eléctrico. No introducir bordes redondeados, sombras suaves, gradientes genéricos ni reemplazar el diseño por una plantilla SaaS.

## Verificación obligatoria

Antes de entregar cambios ejecutar, cuando estén disponibles:

```bash
go mod tidy
go test ./...
go vet ./...
golangci-lint run ./...
docker buildx build --load -t portfolio:verify .
```

Revisar `git diff`, `git diff --check` y `git status`. No incluir `*.db`, `*.log`, `*.pid`, secretos ni archivos temporales.
