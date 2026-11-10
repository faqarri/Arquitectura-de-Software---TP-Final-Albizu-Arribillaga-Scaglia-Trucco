package models

import (
	"strconv"
	"time"
)

// Estados de una retención (ADR-014). Una retención solo es válida si está
// ACTIVA y expira_en > NOW() (reloj de la base), aunque el barrido todavía no la
// haya marcado VENCIDA (RN-11).
const (
	RetencionActiva     = "ACTIVA"
	RetencionConfirmada = "CONFIRMADA"
	RetencionLiberada   = "LIBERADA"
	RetencionVencida    = "VENCIDA"
)

// Estados de disponibilidad de una oferta (RN-13).
const (
	Disponible = "DISPONIBLE"
	Retenida   = "RETENIDA"
	Agotada    = "AGOTADA"
)

// Disponibilidad de una oferta. Nunca se cachea (ADR-007).
type Disponibilidad struct {
	OfertaID          string     `json:"ofertaId"`
	Estado            string     `json:"estado"`
	Libres            int        `json:"libres"`
	ProximaLiberacion *time.Time `json:"proximaLiberacion"`
	Version           int64      `json:"version"`
}

// CalcularDisponibilidad aplica la fórmula de ADR-014:
//
//	libres = cupo_total − vendidos − retenidos_activos
//
// proxima es el menor expira_en de las retenciones activas (nil si no hay).
func CalcularDisponibilidad(ofertaID string, cupoTotal, vendidos, retenidos int, proxima *time.Time, version int64) Disponibilidad {
	libres := max(cupoTotal-vendidos-retenidos, 0)
	d := Disponibilidad{OfertaID: ofertaID, Libres: libres, Version: version}
	switch {
	case libres > 0:
		d.Estado = Disponible
	case retenidos > 0:
		d.Estado = Retenida
		d.ProximaLiberacion = proxima
	default:
		d.Estado = Agotada
	}
	return d
}

// VerificarCupo decide si se pueden retener cantidad cupos de una oferta.
// Devuelve ErrRegla (LIMITE_POR_CARRITO, RN-12) si el titular se pasaría del
// máximo por oferta, o ErrSinCupo (RN-01, RN-13) si no alcanzan los libres.
func VerificarCupo(ofertaID string, cantidad, cupoTotal, vendidos, retenidos, delTitular, maxPorOferta int, proxima *time.Time) error {
	if delTitular+cantidad > maxPorOferta {
		return &ErrRegla{
			Codigo:  CodigoLimitePorCarrito,
			Detalle: "No se pueden retener más de " + strconv.Itoa(maxPorOferta) + " cupos de la misma oferta.",
		}
	}
	libres := max(cupoTotal-vendidos-retenidos, 0)
	if libres >= cantidad {
		return nil
	}
	err := &ErrSinCupo{Codigo: CodigoAgotada, OfertaID: ofertaID, Libres: libres}
	if retenidos > 0 {
		err.Codigo = CodigoRetenida
		err.ProximaLiberacion = proxima
	}
	return err
}

// RetencionCreada es una retención tal como se informa a Reservas.
type RetencionCreada struct {
	ID               string  `json:"id"`
	OfertaID         string  `json:"ofertaId"`
	Cantidad         int     `json:"cantidad"`
	Descripcion      string  `json:"descripcion"`
	Destino          string  `json:"destino"`
	PrecioReferencia float64 `json:"precioReferencia"`
}

// GrupoRetenido son las retenciones creadas por una misma solicitud (una oferta
// suelta, o todas las opciones de un pack). Comparten expiraEn.
type GrupoRetenido struct {
	GrupoID     string            `json:"grupoId"`
	ExpiraEn    time.Time         `json:"expiraEn"`
	Retenciones []RetencionCreada `json:"retenciones"`
}

// LineaConfirmada es una línea de la compra con el precio cobrado (RN-05, RN-06).
type LineaConfirmada struct {
	RetencionID string  `json:"retencionId"`
	OfertaID    string  `json:"ofertaId"`
	Descripcion string  `json:"descripcion"`
	Destino     string  `json:"destino"`
	Precio      float64 `json:"precio"`
}

// Confirmacion es la respuesta de confirmar una compra. Se guarda tal cual para
// devolver lo mismo ante un reintento (idempotencia).
type Confirmacion struct {
	CompraID string            `json:"compraId"`
	Lineas   []LineaConfirmada `json:"lineas"`
	Total    float64           `json:"total"`
}
