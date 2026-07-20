# cmoreno.org - Portfolio Architecture

Plataforma de portafolio personal diseñada con una estética **Tech-Brutalista** y una arquitectura ultra-optimizada para despliegues en producción usando **Coolify** y **Traefik**.

## 🏗 Arquitectura del Sistema

La aplicación utiliza el stack **GOTH** (Go, SQLite, HTMX):
- **Go 1.23+**: Backend de alto rendimiento con enrutamiento nativo y concurrencia segura.
- **SQLite (WAL mode)**: Base de datos embebida ultra-rápida operando como única fuente de verdad.
- **HTMX & Alpine.js**: Frontend reactivo hiperligero, SSR (Server-Side Rendered), sin frameworks SPA pesados.

### Decisiones Críticas
- **Stateless (Excepto DB):** El binario compila el frontend usando `//go:embed`. El único directorio que requiere persistencia (y backup) es `/data`.
- **Graceful Shutdown:** La aplicación captura `SIGTERM` y cierra la base de datos limpiamente antes de apagar el servidor, evitando corrupción en despliegues automatizados de Coolify.
- **Seguridad Integrada:** Middleware con `Content-Security-Policy` estricto, `Permissions-Policy`, y estrategias asimétricas de caché (Etag + Immutable en estáticos; No-cache en HTML).

## 🚀 Despliegue en Coolify

Este repositorio está preparado para funcionar como un **Servicio Docker** directamente en Coolify.

### Configuración en Coolify
1. **Tipo de Build:** Selecciona **Dockerfile** (NO uses Nixpacks para no inflar la imagen).
2. **Volumen Persistente:** En la pestaña *Storage*, mapea un volumen local al directorio `/data` del contenedor. **(⚠️ ESTE ES EL ÚNICO DIRECTORIO QUE REQUIERE BACKUP ⚠️)**
3. **Red:** La aplicación respetará automáticamente los proxies inversos como Traefik leyendo el puerto dinámico de la variable `PORT` (por defecto 8080).

### Variables de Entorno Recomendadas

| Variable | Descripción | Valor Recomendado en Prod |
|----------|-------------|---------------------------|
| `PORT` | Puerto de escucha HTTP | *(Dinámico por Coolify)* |
| `DATABASE_PATH` | Ruta del archivo SQLite | `/data/site.db` |
| `APP_ENV` | Entorno de ejecución | `production` (activa logs JSON) |
| `LOG_LEVEL` | Nivel de logs | `info` o `warn` |

## 💻 Entorno de Desarrollo Local

Para garantizar la paridad exacta entre tu entorno local y el servidor, utilizamos `compose.dev.yaml`.

```bash
# 1. Crear el volumen local (opcional)
mkdir -p data

# 2. Levantar el entorno
docker compose -f compose.dev.yaml up --build
```
La aplicación estará disponible en `http://localhost:8080` y SQLite persistirá localmente en la carpeta `./data/`.

## 🛡 Comandos `make` (Makefile)

- `make run`: Ejecuta el servidor localmente (sin Docker).
- `make build`: Construye el binario inyectando los `-ldflags` (Commit, BuildDate, Version).
- `make test`: Ejecuta toda la suite de pruebas.
- `make docker`: Construye la imagen Docker manualmente.
- `make lint`: Ejecuta el linter estático.

## 📊 Endpoints de Observabilidad

- `GET /health`: Revisa que el servidor HTTP esté activo (usado por Docker HEALTHCHECK).
- `GET /ready`: Hace ping a SQLite para verificar disponibilidad total.
- `GET /version`: Devuelve un JSON con la versión, commit y fecha de compilación de la imagen en ejecución.
- `GET /metrics`: Expone métricas estándar de Go compatibles con **Prometheus**.
