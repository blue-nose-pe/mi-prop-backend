# API Docs — Mi Propósito 2.0

Documentación interactiva de la API REST renderizada con **Stoplight

## 📁 Archivos

```
deploy/api-docs/
├── openapi.yaml   ← spec OpenAPI 3.1 (fuente de verdad — editá ESTE archivo)
├── index.html     ← renderer con Stoplight Elements (CDN, sin build)
└── README.md      ← este archivo
```

## 🚀 Ver la documentación localmente

### Opción 1 — Python (lo más simple)

```powershell
cd deploy/api-docs
python -m http.server 8000
```

Abrir http://localhost:8000

### Opción 2 — Node (si ya tenés Node)

```powershell
cd deploy/api-docs
npx http-server -p 8000
```

### Opción 3 — VS Code Live Server

Click derecho sobre `index.html` → **Open with Live Server**.

> No funciona abriendo `index.html` con doble-clic (`file://`) porque
> el navegador bloquea el `fetch` del `openapi.yaml` por CORS. Hace falta
> servirlo por HTTP.

## 🌐 Publicar la documentación

### A) GitHub Pages (lo más simple, gratis)

```powershell
# Una sola vez: crear branch gh-pages con el contenido de api-docs
git checkout --orphan gh-pages
git rm -rf .
cp -r deploy/api-docs/* .
git add openapi.yaml index.html README.md
git commit -m "docs: api"
git push origin gh-pages
```

GitHub publica automáticamente en `https://<usuario>.github.io/<repo>/`.

### B) Azure Static Web Apps (integrado al ecosistema Azure del proyecto)

```powershell
az staticwebapp create `
  --name miproposito-api-docs `
  --resource-group rg-miproposito `
  --location eastus2 `
  --source https://github.com/<org>/<repo> `
  --branch main `
  --app-location "deploy/api-docs"
```

Te queda en `https://<random>.azurestaticapps.net` (con TLS automático).
Podés mapear un dominio: `docs.miproposito.ucsp.edu.pe`.

### C) Servirlo desde el propio gateway

Si querés que la doc viva en `https://api.miproposito.ucsp.edu.pe/docs`,
agregar al gateway un handler de archivos estáticos:

```go
// gateway/internal/proxy/proxy.go
mux.Handle("/docs/", http.StripPrefix("/docs/", http.FileServer(http.Dir("api-docs"))))
```

Y copiar `deploy/api-docs/*` al sistema de archivos del pod (volume mount
o `COPY` en el `Dockerfile`).

> **Recomendado**: usar opción A o B mientras la API está en desarrollo
> (la doc cambia rápido y no querés re-deployar el gateway por cada
> cambio de doc). Cuando la API se estabilice, mover a C para tener una
> sola URL pública.

## 🛠️ Editar la documentación

**Toda la documentación vive en `openapi.yaml`.** El `index.html` solo lo
renderiza — no hay HTML por endpoint que mantener.

Workflow típico:

1. Editar `openapi.yaml` (agregar endpoint, cambiar schema, agregar ejemplo)
2. Validar:

   ```powershell
   # Spectral (linter oficial de OpenAPI)
   npx @stoplight/spectral-cli lint deploy/api-docs/openapi.yaml
   ```

3. Refrescar `localhost:8000` → ver el cambio

### Convenciones que usamos

- **Tags**: agrupan endpoints en el sidebar. Mantenerlos cortos y consistentes
  (`Auth`, `Users`, `Exams`, etc.).
- **Ejemplos múltiples**: cada endpoint clave tiene 2-3 ejemplos en
  `examples:` con un `summary` descriptivo. El renderer los muestra como
  pestañas.
- **Schemas reutilizables**: cualquier estructura que aparezca más de una
  vez va a `components.schemas.*` y se referencia con `$ref`. Evita
  duplicación y mantiene la doc consistente.
- **Errores documentados**: cada response 4xx/5xx tiene un schema
  `ErrorResponse` con `example` real (no genérico).

## 🤝 Generar clientes desde el spec

El `openapi.yaml` puede generar SDKs automáticamente:

```powershell
# TypeScript / Angular para el frontend
npx @openapitools/openapi-generator-cli generate `
  -i deploy/api-docs/openapi.yaml `
  -g typescript-angular `
  -o sdks/angular

# Go para integraciones internas
npx @openapitools/openapi-generator-cli generate `
  -i deploy/api-docs/openapi.yaml `
  -g go `
  -o sdks/go

# Postman collection
npx openapi-to-postmanv2 -s deploy/api-docs/openapi.yaml -o miproposito.postman_collection.json
```

## 📋 Cobertura actual

| Tag             | Endpoints documentados | Status backend                                |
|-----------------|------------------------|-----------------------------------------------|
| Auth            | 3                      | ✅ implementado en gateway                    |
| Users           | 9                      | 🟡 documentado, falta wirear gateway proxy    |
| Schools         | 1                      | 🟡 documentado, falta wirear gateway proxy    |
| Exams           | 7                      | 🟡 documentado, falta wirear gateway proxy    |
| Questions       | 7                      | 🟡 documentado, falta wirear gateway proxy    |
| ExamQuestions   | 4                      | 🟡 documentado, falta wirear gateway proxy    |
| Attempts        | 5                      | 🟡 documentado, falta wirear gateway proxy    |
| Keys            | 8                      | 🟡 documentado, falta wirear gateway proxy    |
| Surveys         | 13                     | 🟡 documentado, falta wirear gateway proxy    |
| Analytics       | 8                      | 🟡 documentado, falta wirear gateway proxy    |
| HubSpot         | 7                      | 🟡 documentado, falta wirear gateway proxy    |
| **Total**       | **72**                 | Auth funcional; resto: contrato listo, proxy en sprint siguiente |

> **El contrato está fijado**. El frontend puede empezar a tipar y mockear
> contra esta doc; cuando los proxies se vayan agregando al gateway, las
> rutas funcionarán sin cambiar el contrato (los `.proto` de los servicios
> ya están alineados con el spec).

## 🔧 Troubleshooting

| Síntoma                                   | Causa                                  | Solución                                            |
|-------------------------------------------|----------------------------------------|------------------------------------------------------|
| Página en blanco                          | Browser bloqueó CORS al hacer fetch    | Usar `python -m http.server` (no `file://`)         |
| "Failed to load OpenAPI spec"             | YAML inválido o ruta mal               | Validar con `spectral lint openapi.yaml`            |
| "Try It" devuelve CORS error              | Server gateway sin allow `https://docs...` | Agregar el origen a `gateway`: `CORS_ALLOWED_ORIGINS` |
| Endpoints no aparecen en sidebar          | Tag no declarado en `tags:` global     | Agregar el tag en la sección `tags:` arriba del YAML |
| Cambios no se reflejan                    | Stoplight Elements cachea el yaml      | Hard refresh (Ctrl+Shift+R) o agregar `?v=2` al URL |
