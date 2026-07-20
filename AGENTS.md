# Contexto de Sistema para Agentes de IA

Este documento define las reglas estrictas de arquitectura y diseño para cualquier agente de IA (como Claude, ChatGPT, Cursor, Copilot) que asista en este repositorio.

## 1. Stack Inmutable (GOTH)
- **Backend:** Go (Librería estándar `net/http`, cero frameworks).
- **Base de Datos:** SQLite (`github.com/mattn/go-sqlite3`). Única fuente de verdad.
- **Frontend Interactivo:** HTMX (Cero frameworks SPA como React o Vue).
- **Frontend Lógica:** Alpine.js (Solo para observers, estados de UI efímeros y validaciones).

## 2. Inmutabilidad del Frontend (Regla de Oro)
- **NO TOCAR HTMX:** Está estrictamente prohibido modificar atributos `hx-*`, cambiar las rutas de los endpoints HTML o renombrar IDs usados para el targeting. Los agentes tienden a querer "optimizar" o migrar esto a JSON; eso rompería el sistema.
- **Estética Tech-Brutalista:** El diseño usa `Instrument Serif` y `Space Mono`. No se permiten bordes redondeados (`border-radius`), sombras suaves (`box-shadow`), ni colores genéricos (morados/gradients). Se usa un esquema de alto contraste con acentos (Negro, Blanco, Azul Eléctrico).

## 3. Arquitectura y Coolify
- **Despliegue:** La app corre detrás de Traefik en Coolify.
- **Estado (State):** El contenedor Docker es 100% efímero (Stateless). El único estado persistente vive en el volumen montado en `/data` (donde reside SQLite).
- **Configuración:** Todo se configura por variables de entorno (`PORT`, `DATABASE_PATH`, `APP_ENV`, `LOG_LEVEL`). NUNCA se asume un puerto fijo (usar siempre la variable `PORT`).
- **Archivos Estáticos:** `templates` y `static` están incrustados (`//go:embed`) en el binario. No se montan volúmenes para servir el frontend.
