# Capacidad consumida: Estudiario (Grupo 2)

Registro y consulta de documentos PDF con moderación, que usamos para las **guías de viaje** (HU-13 y HU-14). Decisión y uso en [ADR-009](../../../adr/ADR-009-consumo-del-proveedor.md).

| Dato | Valor |
|---|---|
| Proveedor | Estudiario, Grupo 2 ([repositorio](https://github.com/MichiSil/ProyectoEstudiario)) |
| Guía del proveedor | [docs/contracts/README.md](https://github.com/MichiSil/ProyectoEstudiario/blob/main/docs/contracts/README.md) |
| Contrato publicado | [documents-v1.yaml](https://github.com/MichiSil/ProyectoEstudiario/blob/main/docs/contracts/documents-v1.yaml) |
| **Versión fijada** | **1.0.0**, en [1.0.0/documents-v1.yaml](1.0.0/documents-v1.yaml), copiada el 2026-11-07 junto con su PDF de ejemplo |
| URL pública | Pendiente de que la publique el proveedor. Va en `ESTUDIARIO_URL` del `.env` |
| API Key | La entrega el proveedor por privado. Va en `ESTUDIARIO_API_KEY` del `.env`, **nunca** en el repositorio |
| Mock local | `estudiario-mock` en el docker-compose (`http://localhost:4030`), con la versión fijada |
| Adaptador | [`pkg/estudiario`](../../../../pkg/estudiario) |

## Test de contrato

En `pkg/estudiario` (`go test ./...` desde `pkg/`):

- **`contrato_test.go`**: un mock armado con `kin-openapi` a partir de la versión fijada.
  - Valida contra el contrato **cada solicitud** del adaptador: registrar (multipart con el PDF), buscar y listar las registradas.
  - Responde con **todos los ejemplos** del contrato: los éxitos y los errores `401`, `409`, `413`, `415`, `422`, `429` y `503`. El adaptador los tiene que interpretar bien, ya sea como "reintentar más tarde" o como "rechazo de los datos".
  - Verifica que los ejemplos del propio contrato cumplan su esquema.
- **`TestContratoPublicadoSigueCompatible`**: descarga el contrato que publica el proveedor y falla si:
  - cambió la versión mayor,
  - desapareció una operación que usamos, o
  - un campo del documento que leemos dejó de ser obligatorio.

  Necesita internet: se saltea con `-short` o sin conexión.

## Cómo actualizar la versión

1. Copiar el YAML nuevo a `docs/contracts/consumidos/estudiario/<versión>/`.
2. Apuntar `rutaContrato` de `pkg/estudiario/contrato_test.go` a la carpeta nueva y correr los tests.
3. Si es una versión mayor (`/v2`), adaptar el adaptador en un Pull Request propio y registrar el cambio en ADR-009.
