package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// claveConsumidor es donde autenticar deja el nombre del grupo consumidor en el
// contexto de Gin, para que los handlers lo pasen a los casos de uso.
const claveConsumidor = "consumidor"

// autenticar: TODO(api-mysql). Leer X-API-Key, buscarla en apiKeys y guardar el
// grupo con c.Set(claveConsumidor, grupo). Si falta o no existe: 401 problem+json.
func autenticar(apiKeys map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

// limitarSolicitudes: TODO(api-mysql). 60 solicitudes por minuto por consumidor
// (en memoria, por instancia). Al superarlo: 429 problem+json con Retry-After.
func limitarSolicitudes() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

// emitir: TODO(api-mysql). POST /v1/comprobantes.
//   - Exigir Idempotency-Key (8 a 64 caracteres) y validar el cuerpo según el
//     contrato, sin campos adicionales: 400 con la lista de errores.
//   - Leer los precios como decimal.Decimal (no float64) para no perder precisión.
//   - Llamar a d.Emitir.Ejecutar y responder 201 (con header Location) o 200 si
//     YaExistia; ErrConflictoIdempotencia → 409; domain.ErrTotalCero → 422.
func emitir(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		problema(c, http.StatusNotImplemented, "no-implementado", "No implementado", "")
	}
}

// obtener: TODO(api-mysql). GET /v1/comprobantes/{id} → d.Obtener.Ejecutar con el
// consumidor autenticado; ports.ErrNoEncontrado → 404.
func obtener(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		problema(c, http.StatusNotImplemented, "no-implementado", "No implementado", "")
	}
}

// problema responde un error con formato application/problem+json (RFC 9457).
func problema(c *gin.Context, estado int, tipo, titulo, detalle string) {
	cuerpo := gin.H{
		"type":   "https://agencia-viajes.example/errores/" + tipo,
		"title":  titulo,
		"status": estado,
	}
	if detalle != "" {
		cuerpo["detail"] = detalle
	}
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(estado, cuerpo)
}
