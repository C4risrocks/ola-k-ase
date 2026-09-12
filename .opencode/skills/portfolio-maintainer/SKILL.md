---
name: portfolio-maintainer
description: Úsala cuando se modifique el repositorio ola-k-ase (portafolio cmoreno.org) — Go net/http, SQLite, HTMX, Alpine.js, assets embebidos, Docker/Coolify — para respetar el stack inmutable, los contratos HTMX/Alpine, las migraciones, la persistencia en /data, la seguridad y la verificación obligatoria. Use when editing this repo's Go handlers, SQLite schema, HTMX partials, Alpine state, Dockerfile or CI.
---

# Portfolio Maintainer

Guía operativa del repositorio `ola-k-ase` (portafolio cmoreno.org). Es complementaria a `AGENTS.md`; si algo difiere, manda `AGENTS.md`.

## Mapa del proyecto

- `main.go`: arranque, rutas, templates, handlers, endpoints operativos y graceful shutdown.
- `admin_command.go`: subcomando `admin set-password` (Argon2id, sin credenciales por defecto).
- `security.go`: validación de origen, guardia CSRF, cookies de sesión y CSRF.
- `ratelimit.go`: rate limiting en memoria; `clientIP` usa `RemoteAddr` y la última entrada de `X-Forwarded-For` solo si el peer directo es loopback o privado.
- `post_form.go`: validación de posts, tags y slugs.
- `database.go`: conexión SQLite, migraciones, seeds, auth, sesiones y acceso a datos.
- `middleware.go`: recovery, request ID, logs, headers de seguridad, gzip, caché y ETags.
- `config.go`: `PORT`, `DATABASE_PATH`, `APP_ENV`, `LOG_LEVEL`.
- `migrations/`: SQL embebido, append-only.
- `templates/`: `index.html` y partials HTMX.
- `static/`: CSS, JavaScript y fuentes embebidos.
- `main_test.go`: tests de integración con SQLite temporal y `httptest`.

## Stack inmutable

- Go y `net/http` estándar, sin frameworks de routing.
- SQLite con `github.com/mattn/go-sqlite3` (CGO habilitado solo en el builder).
- HTMX y Alpine.js vendorizados en `static/`; sin bundlers, npm, CDNs ni SPA.
- Templates, CSS e imágenes embebidos con `//go:embed`.

## Contratos HTMX/Alpine (no romper)

- No renombrar rutas ni IDs usados como `hx-target`.
- No cambiar atributos `hx-*`, `hx-trigger`, `hx-swap` ni el HTML que esperan los partials.
- Nunca responder JSON en flujos HTMX.
- Un mismo elemento no debería declarar `hx-post` y `hx-get` a la vez; existe ese caso en `templates/partials/admin_dashboard.html`.
- Las secciones de `templates/index.html` usan `hx-trigger="none"`; el retry actual dispara `revealed` y probablemente no recarga. Para recargar manualmente, usar `htmx.ajax(...)`.
- Alpine solo para estado efímero, observers y validación.

## Persistencia y migraciones

- El contenedor es stateless; `/data` es el único volumen persistente.
- Default `DATABASE_PATH=/data/site.db`.
- Migraciones append-only en `migrations/`; nunca editar una aplicada, crear otra con número mayor.
- Mantener WAL, `synchronous=NORMAL`, foreign keys, busy timeout y `SetMaxOpenConns(1)`; verificar el comportamiento del pool antes de cambiarlo.
- No incluir `*.db`, `*.log`, `*.pid`, secretos ni temporales en commits.

## Seguridad

- No existe credencial por defecto: el administrador se provisiona con `portfolio admin set-password` (Argon2id y revocación de sesiones).
- No reintroducir hashes SHA-256 propios; `parsePasswordHash` solo acepta Argon2id con parámetros dentro de rango.
- Las sesiones se almacenan solo como hash SHA-256; nunca persistir tokens en claro. `createSession` devuelve `(token, csrfToken)`.
- Toda mutación administrativa pasa por `requireAdminMutation` (método, `Origin`/`Referer`, sesión y `X-CSRF-Token`).
- No confiar en `X-Forwarded-*` para seguridad. En rate limiting, `clientIP` solo acepta la última entrada de `X-Forwarded-For` si el peer directo es loopback o privado; nunca confiar en entradas anteriores.
- Respetar los límites de body y de campos de `main.go`, `post_form.go` y `ratelimit.go`.
- Los assets frontend están vendorizados en `static/`; no reintroducir CDNs ni ampliar `script-src` con `'unsafe-inline'`. `unsafe-eval` sigue pendiente de la build CSP de Alpine.
- `/data` debe quedar en `0700` y la base en `0600`; el arranque falla si no se pueden aplicar.

## Operación y despliegue

- Variables permitidas: `PORT`, `DATABASE_PATH`, `APP_ENV`, `LOG_LEVEL`, con defaults razonables y sin `panic()` ni `log.Fatal()`.
- Mantener operativos `HEALTHCHECK`, `/health`, `/ready`, `/version` y `/metrics`.
- Docker multi-stage con BuildKit, usuario no privilegiado UID 10001, detrás de Traefik y sin asumir HTTPS.

## Frontend

- Estética: `Instrument Serif` + `Space Mono`, bordes duros de 1px y paleta negro, blanco y azul eléctrico.
- No introducir bordes redondeados, sombras suaves, gradientes genéricos ni plantillas SaaS.

## Verificación antes de entregar

```bash
go mod tidy
go test ./...
go vet ./...
golangci-lint run ./...
docker buildx build --load -t portfolio:verify .
```

Revisar además `git diff`, `git diff --check` y `git status`; no dejar artefactos ni secretos.

## Skills relacionadas

- `golang-security`, `golang-database`, `golang-testing` (instaladas desde Context7 `/samber/cc-skills-golang`).
- Globales: `htmx`, `sqlite-database-expert`, `secrets-audit`, `codebase-memory`.
