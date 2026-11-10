package handlers

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"agencia/catalogo/internal/models"
)

// GET /api/busqueda (HU-01, ADR-006). Pública. Los resultados no incluyen la
// disponibilidad: se consulta en el detalle.
func buscar(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		f, err := filtrosDeConsulta(c)
		if err != nil {
			responderError(c, d, err)
			return
		}
		pagina, err := d.Busqueda.Buscar(c.Request.Context(), f)
		if err != nil {
			responderError(c, d, err)
			return
		}
		c.JSON(http.StatusOK, pagina)
	}
}

// filtrosDeConsulta lee los parámetros. Los números mal escritos son errores de
// validación; el resto lo revisa models.FiltrosBusqueda.Validar.
func filtrosDeConsulta(c *gin.Context) (models.FiltrosBusqueda, error) {
	f := models.FiltrosBusqueda{
		Q:                 c.Query("q"),
		Destino:           c.Query("destino"),
		Tipo:              models.TipoOferta(c.Query("tipo")),
		SubtipoTransporte: models.SubtipoTransporte(c.Query("subtipoTransporte")),
		Desde:             strings.TrimSpace(c.Query("desde")),
		Hasta:             strings.TrimSpace(c.Query("hasta")),
		Orden:             models.OrdenBusqueda(strings.TrimSpace(c.Query("orden"))),
	}
	var errs []models.ErrorCampo
	entero := func(campo string, destino *int) {
		if v := strings.TrimSpace(c.Query(campo)); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				errs = append(errs, models.ErrorCampo{Campo: campo, Mensaje: "Tiene que ser un número entero."})
				return
			}
			*destino = n
			if n == 0 {
				*destino = -1 // 0 explícito es inválido; Validar lo informa
			}
		}
	}
	decimal := func(campo string) *float64 {
		v := strings.TrimSpace(c.Query(campo))
		if v == "" {
			return nil
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			errs = append(errs, models.ErrorCampo{Campo: campo, Mensaje: "Tiene que ser un número."})
			return nil
		}
		return &n
	}
	entero("pagina", &f.Pagina)
	entero("tamanio", &f.Tamanio)
	f.PrecioMin = decimal("precioMin")
	f.PrecioMax = decimal("precioMax")
	if len(errs) == 0 {
		return f, nil
	}
	// Se informan juntos los números mal escritos y el resto de los problemas.
	var v *models.ErrValidacion
	if err := f.Validar(); errors.As(err, &v) {
		for _, e := range v.Errores {
			if !slices.ContainsFunc(errs, func(x models.ErrorCampo) bool { return x.Campo == e.Campo }) {
				errs = append(errs, e)
			}
		}
	}
	return f, &models.ErrValidacion{Errores: errs}
}

// POST /internal/busqueda/reconstruccion (ADR-006). Operación manual: arma un
// índice nuevo con todo MongoDB y le pasa el alias sin cortar la búsqueda. No
// se publica en el gateway.
func reconstruirIndice(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Si el cliente corta la conexión, la reconstrucción termina igual.
		res, err := d.Busqueda.Reconstruir(context.WithoutCancel(c.Request.Context()))
		if err != nil {
			responderError(c, d, err)
			return
		}
		c.JSON(http.StatusOK, res)
	}
}
