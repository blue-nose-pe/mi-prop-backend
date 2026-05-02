# Mi Propósito 2.0 — Backend

Plataforma de orientación vocacional, simulacro de admisión y hábitos de
estudio de la **UCSP** (Universidad Católica San Pablo, Arequipa).

Este repositorio contiene **el backend completo**: 7 microservicios en
Go, infraestructura Azure declarativa (Bicep), Helm charts para AKS y
documentación OpenAPI publicada en GitHub Pages.

> El frontend Angular vive en otro repositorio (`ucsp-front`) y se
> mantiene por separado. La sincronización con HubSpot es bidireccional
> y no negociable (requisito TDR).

## 📐 Arquitectura

```
                      ┌─────────────────────────────┐
                      │  ucsp-front (Angular 18)    │
                      │  HTTPS REST                 │
                      └──────────────┬──────────────┘
                                     │
                      ┌──────────────▼──────────────┐
                      │  AKS Ingress (NGINX + TLS)  │
                      └──────────────┬──────────────┘
                                     │ HTTPS REST
                      ┌──────────────▼──────────────┐
                      │  gateway (Go)               │  ◄── REST → gRPC
                      │  JWT, CORS, rate-limit      │
                      └──┬──────┬──────┬──────┬─────┘
                         │ gRPC │ gRPC │ gRPC │ gRPC ...
       ┌─────────────────▼─┐  ┌─▼─────┐  ┌─▼─────┐  ┌──────────────┐
       │ users-service     │  │ exams │  │ keys  │  │ hubspot      │
       │ Go + Azure SQL    │  │ Go+SQL│  │ Go+SQL│  │ Go + asynq   │
       └───────────────────┘  └───────┘  └───────┘  └──────────────┘
                                  ▲
                                  │ gRPC clients (cross-service)
       ┌──────────────────┐  ┌────┴──────────┐
       │ satisfaction     │  │ analytics     │
       │ Go + Azure SQL   │  │ Go (sin DB)   │
       └──────────────────┘  └───────────────┘
```

Patrón aplicado en TODOS los servicios: **hexagonal + CQRS + SOLID**. Ver
[ARCHITECTURE.md](ARCHITECTURE.md) para las 7 reglas de oro y el flujo de
desarrollo.

## 📦 Servicios

| Servicio | Stack | Responsabilidad |
|---|---|---|
| [`users_service`](users_service/) | Go + Azure SQL | Identidad, JWT (access 15 min + refresh 7 días con rotación), permisos granulares, audit log, asignaciones históricas (SCD-2), `hubspot_record_id` |
| [`exams_service`](exams_service/) | Go + Azure SQL | Los 3 módulos (vocacional/simulacro/hábitos) como `exam_type`, versionado por linaje (`parent_exam_id`), attempts y respuestas |
| [`keys_service`](keys_service/) | Go + Azure SQL | Códigos de acceso a tests (modo `school` / `lan`), `IncrementUses` atómico, ventanas temporales |
| [`hubspot_service`](hubspot_service/) | Go + Redis (asynq) | Sync bidireccional con HubSpot, OTP webhook, custom objects (Key/Asesor/Colegio), backoff exponencial |
| [`satisfaction_service`](satisfaction_service/) | Go + Azure SQL | Encuestas (5 tipos: `scale`, `nps`, `single`, `multi`, `open`), métricas calculadas en core (NPS, average, distribución) |
| [`analytics_service`](analytics_service/) | Go + Redis | Dashboards agregados (asesor/colegio/estudiante), comparativos, exports XLSX. Sin DB propia |
| [`gateway`](gateway/) | Go | Punto de entrada HTTP. Traduce REST→gRPC + JWT validation + CORS + rate-limit Redis |

## 📚 Documentación de la API

**[https://blue-nose-pe.github.io/mi-prop-backend/](https://blue-nose-pe.github.io/mi-prop-backend/)** ← Docs interactivas (Stoplight Elements: 3 columnas, search, "try it", code samples).

Se publican automáticamente al pushear cambios en `deploy/api-docs/`
(GitHub Actions). Spec OpenAPI 3.1 en
[`deploy/api-docs/openapi.yaml`](deploy/api-docs/openapi.yaml) — fuente
única de verdad para el contrato. **69 endpoints, 28 schemas**.

## 🚀 Deploy a Azure

Guía paso a paso (PowerShell): [`deploy/INSTALL.md`](deploy/INSTALL.md).

Resumen:
1. **Bicep** crea Azure SQL Server + 4 BDs + Redis + Key Vault + ACR.
2. **AKS cluster** con addon `azure-keyvault-secrets-provider`.
3. **Helm umbrella chart** despliega los 7 microservicios + corre Jobs
   de migración T-SQL automáticamente (idempotentes con checksum SHA-256).
4. **Smoke tests** verifican el deploy end-to-end.

CI/CD vía Azure DevOps en [`azure-pipelines.yml`](azure-pipelines.yml).

## 🛠️ Desarrollo local

```powershell
# Workspace Go con todos los servicios
go build ./users_service/...
go test  ./users_service/internal/core/...

# Regenerar protobuf (si tocaste un .proto)
cd users_service ; buf generate

# Ver la documentación localmente
cd deploy/api-docs ; python -m http.server 8000
# Abrir http://localhost:8000
```

## 📂 Layout del repositorio

```
.
├── ARCHITECTURE.md             ← arquitectura canónica (hexagonal + CQRS + 7 reglas)
├── README.md                   ← este archivo
├── azure-pipelines.yml         ← CI/CD Azure DevOps
├── go.work                     ← Go workspace (los 7 servicios Go)
│
├── users_service/              ← cmd/{server,migrate} + internal/{core,adapters,shared} + db/migrations + proto
├── exams_service/              ← idem (mismo patrón hexagonal+CQRS)
├── keys_service/
├── hubspot_service/            ← cmd/{server,worker} + internal/{core,adapters} + proto
├── satisfaction_service/
├── analytics_service/          ← sin db/, sin migrate
├── gateway/                    ← cmd/server + internal/{middleware,proxy}
│
└── deploy/
    ├── bicep/main.bicep        ← infra Azure (SQL, Redis, Key Vault, ACR)
    ├── helm/
    │   ├── miproposito/        ← umbrella chart
    │   └── charts/             ← 7 subcharts (uno por microservicio)
    ├── api-docs/               ← OpenAPI spec + Stoplight Elements (publicado en GitHub Pages)
    ├── INSTALL.md              ← guía de instalación PowerShell
    ├── smoke-test.ps1          ← verificación post-deploy (Windows)
    └── smoke-test.sh           ← idem (Linux, lo usa el pipeline)
```

## 🔑 Stack canónico (no negociable)

- **Lenguaje único**: Go 1.24
- **RPC interno**: gRPC + Protocol Buffers (regenerado con `buf`)
- **BD**: Azure SQL Database (T-SQL), una BD por servicio
- **Cache / cola**: Redis (Azure Cache for Redis)
- **Auth**: JWT HS256 (`Authorization: Bearer <token>`)
- **Despliegue**: Helm + Bicep en Azure AKS
- **CI/CD**: Azure DevOps Pipelines
- **Identificador de negocio**: DNI (`document_number`)

## 📝 Licencia

Propietario UCSP. Todo el código y la documentación son propiedad de la
Universidad Católica San Pablo y no son transferibles a otra institución.

## 👥 Contacto

- **Lead técnico**: Pablo Pérez — pablo.perez@bluenose.pe
- **Cliente**: UCSP, Arequipa, Perú
