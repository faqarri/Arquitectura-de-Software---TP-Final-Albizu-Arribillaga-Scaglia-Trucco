package services

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"agencia/catalogo/internal/models"
	"agencia/catalogo/internal/repository"
)

// ConfigRetenciones: RETENCION_MINUTOS (15) y RETENCION_MAX_CUPOS_POR_OFERTA (6).
type ConfigRetenciones struct {
	Minutos      int
	MaxPorOferta int
}

// Retenciones es la API interna que usa Reservas: retener, liberar y confirmar
// cupos (ADR-014, docs/contracts/internos/catalogo-retenciones.yaml).
type Retenciones struct {
	ofertas *repository.Ofertas
	packs   *repository.Packs
	inv     *repository.Inventario
	cfg     ConfigRetenciones
}

func NuevoRetenciones(ofertas *repository.Ofertas, packs *repository.Packs, inv *repository.Inventario, cfg ConfigRetenciones) *Retenciones {
	return &Retenciones{ofertas: ofertas, packs: packs, inv: inv, cfg: cfg}
}

// SolicitudRetencion: ofertaId o packId (con opciones), excluyentes.
type SolicitudRetencion struct {
	IdempotencyKey string
	TitularID      string   `json:"titularId"`
	OfertaID       string   `json:"ofertaId"`
	Cantidad       int      `json:"cantidad"`
	PackID         string   `json:"packId"`
	Opciones       []string `json:"opciones"`
}

func (s *SolicitudRetencion) validar(maxPorOferta int) error {
	var errs []models.ErrorCampo
	if l := len(s.IdempotencyKey); l < 8 || l > 64 {
		errs = append(errs, models.ErrorCampo{Campo: "Idempotency-Key", Mensaje: "El header Idempotency-Key es obligatorio (8 a 64 caracteres)."})
	}
	if strings.TrimSpace(s.TitularID) == "" {
		errs = append(errs, models.ErrorCampo{Campo: "titularId", Mensaje: "Falta el titular de la retención."})
	}
	if (s.OfertaID == "") == (s.PackID == "") {
		errs = append(errs, models.ErrorCampo{Campo: "ofertaId", Mensaje: "Indicá una oferta (ofertaId) o un pack (packId), no ambos."})
	}
	if s.Cantidad == 0 {
		s.Cantidad = 1
	}
	if s.Cantidad < 1 {
		errs = append(errs, models.ErrorCampo{Campo: "cantidad", Mensaje: "La cantidad debe ser al menos 1."})
	}
	if len(errs) > 0 {
		return &models.ErrValidacion{Errores: errs}
	}
	if s.Cantidad > maxPorOferta {
		return &models.ErrRegla{Codigo: models.CodigoLimitePorCarrito,
			Detalle: "No se pueden retener más de " + strconv.Itoa(maxPorOferta) + " cupos de la misma oferta."}
	}
	return nil
}

// Retener retiene cupo de una oferta suelta, o de todas las opciones elegidas
// de un pack en una sola transacción (RN-07). Las reglas de pack (RN-03, RN-04)
// se validan con los datos de MongoDB, nunca con la caché (ADR-007).
func (s *Retenciones) Retener(ctx context.Context, sol SolicitudRetencion) (models.GrupoRetenido, error) {
	if err := sol.validar(s.cfg.MaxPorOferta); err != nil {
		return models.GrupoRetenido{}, err
	}
	pedido := repository.SolicitudRetencion{
		IdempotencyKey: sol.IdempotencyKey, TitularID: sol.TitularID, PackID: sol.PackID,
		Minutos: s.cfg.Minutos, MaxPorOferta: s.cfg.MaxPorOferta,
	}

	if sol.OfertaID != "" {
		o, err := s.ofertas.Obtener(ctx, sol.OfertaID)
		if errors.Is(err, models.ErrNoEncontrado) || (err == nil && !o.Publicada) {
			return models.GrupoRetenido{}, &models.ErrOfertaInexistente{ID: sol.OfertaID}
		}
		if err != nil {
			return models.GrupoRetenido{}, err
		}
		pedido.Items = []repository.ItemRetencion{item(o, sol.Cantidad, 0)}
		return s.inv.Retener(ctx, pedido)
	}

	p, err := s.packs.Obtener(ctx, sol.PackID)
	if errors.Is(err, models.ErrNoEncontrado) {
		return models.GrupoRetenido{}, &models.ErrOfertaInexistente{ID: sol.PackID}
	}
	if err != nil {
		return models.GrupoRetenido{}, err
	}
	opciones := sinRepetir(sol.Opciones)
	encontradas, err := s.ofertas.ObtenerVarias(ctx, opciones)
	if err != nil {
		return models.GrupoRetenido{}, err
	}
	elegidas := make([]models.Oferta, 0, len(opciones))
	for _, id := range opciones {
		o, ok := encontradas[id]
		if !ok || !o.Publicada {
			return models.GrupoRetenido{}, &models.ErrRegla{Codigo: models.CodigoPackInvalido,
				Detalle: "La opción " + id + " ya no está disponible."}
		}
		elegidas = append(elegidas, o)
	}
	if err := models.ValidarEleccion(p, elegidas); err != nil {
		return models.GrupoRetenido{}, err
	}
	for _, o := range elegidas {
		pedido.Items = append(pedido.Items, item(o, sol.Cantidad, p.DescuentoPorcentaje))
	}
	return s.inv.Retener(ctx, pedido)
}

func item(o models.Oferta, cantidad int, descuento float64) repository.ItemRetencion {
	return repository.ItemRetencion{
		OfertaID: o.ID, Cantidad: cantidad, Descripcion: descripcion(o), Destino: o.Destino,
		PrecioReferencia: models.PrecioLinea(o.Precio, cantidad, descuento),
	}
}

// descripcion es como se ve el ítem en el carrito y en el comprobante,
// por ejemplo "Vuelo Córdoba - Bariloche, 12/07".
func descripcion(o models.Oferta) string {
	return o.Titulo + ", " + o.Desde.Format("02/01")
}

// Liberar es idempotente: si la retención no existe o ya no está vigente, no hace nada.
func (s *Retenciones) Liberar(ctx context.Context, retencionID string) error {
	return s.inv.Liberar(ctx, retencionID)
}

// SolicitudConfirmacion: confirmar todas las retenciones de una compra.
type SolicitudConfirmacion struct {
	CompraID     string   `json:"compraId"`
	TitularID    string   `json:"titularId"`
	RetencionIDs []string `json:"retencionIds"`
}

// Confirmar confirma todas las retenciones de la compra o ninguna (RN-02), con
// los precios vigentes al confirmar (RN-06) y el descuento de pack (RN-05).
// Es idempotente por compraId.
func (s *Retenciones) Confirmar(ctx context.Context, sol SolicitudConfirmacion) (models.Confirmacion, error) {
	var errs []models.ErrorCampo
	if strings.TrimSpace(sol.CompraID) == "" || len(sol.CompraID) > 64 {
		errs = append(errs, models.ErrorCampo{Campo: "compraId", Mensaje: "Falta el id de la compra."})
	}
	if strings.TrimSpace(sol.TitularID) == "" {
		errs = append(errs, models.ErrorCampo{Campo: "titularId", Mensaje: "Falta el titular."})
	}
	sol.RetencionIDs = sinRepetir(sol.RetencionIDs)
	if len(sol.RetencionIDs) == 0 {
		errs = append(errs, models.ErrorCampo{Campo: "retencionIds", Mensaje: "Indicá al menos una retención."})
	}
	if len(errs) > 0 {
		return models.Confirmacion{}, &models.ErrValidacion{Errores: errs}
	}

	// Reintento de una compra ya confirmada: la misma respuesta, sin tocar MongoDB.
	if guardada, err := s.inv.ConfirmacionGuardada(ctx, sol.CompraID); err != nil || guardada != nil {
		if guardada != nil {
			return *guardada, nil
		}
		return models.Confirmacion{}, err
	}

	datos, err := s.inv.DatosRetenciones(ctx, sol.RetencionIDs)
	if err != nil {
		return models.Confirmacion{}, err
	}
	var ofertaIDs []string
	descuentos := map[string]float64{}
	for _, d := range datos {
		ofertaIDs = append(ofertaIDs, d.OfertaID)
		if d.PackID != "" {
			descuentos[d.PackID] = 0
		}
	}
	for packID := range descuentos {
		p, err := s.packs.Obtener(ctx, packID)
		if err != nil {
			return models.Confirmacion{}, err
		}
		descuentos[packID] = p.DescuentoPorcentaje
	}
	ofertas, err := s.ofertas.ObtenerVarias(ctx, ofertaIDs)
	if err != nil {
		return models.Confirmacion{}, err
	}

	lineas := map[string]models.LineaConfirmada{}
	for _, d := range datos {
		o, ok := ofertas[d.OfertaID]
		if !ok {
			continue // sin precio: el repositorio la informa como no vigente
		}
		desc := descripcion(o)
		if d.Cantidad > 1 {
			desc += " x" + strconv.Itoa(d.Cantidad)
		}
		lineas[d.ID] = models.LineaConfirmada{
			Descripcion: desc, Destino: o.Destino,
			Precio: models.PrecioLinea(o.Precio, d.Cantidad, descuentos[d.PackID]),
		}
	}
	return s.inv.Confirmar(ctx, sol.CompraID, sol.TitularID, sol.RetencionIDs, lineas)
}

// VencerPendientes corre el barrido de retenciones vencidas.
func (s *Retenciones) VencerPendientes(ctx context.Context) (int, error) {
	return s.inv.VencerPendientes(ctx, 100)
}

func sinRepetir(ids []string) []string {
	vistos := map[string]bool{}
	var res []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !vistos[id] {
			vistos[id] = true
			res = append(res, id)
		}
	}
	return res
}
