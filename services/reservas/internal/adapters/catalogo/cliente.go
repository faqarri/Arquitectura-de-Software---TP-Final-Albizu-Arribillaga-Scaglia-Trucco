// Package catalogo implementa ports.RetencionesCatalogo contra la API interna de
// Catálogo (docs/contracts/internos/catalogo-retenciones.yaml).
package catalogo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"agencia/pkg/observabilidad"
	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

// Config del cliente (ADR-005: timeout de 2 s por intento y hasta 2 reintentos
// con backoff de 200 y 400 ms, con jitter).
type Config struct {
	URL     string
	Timeout time.Duration
	Esperas []time.Duration
	HTTP    *http.Client
}

type Cliente struct {
	cfg Config
}

var _ ports.RetencionesCatalogo = (*Cliente)(nil)

func NuevoCliente(cfg Config) *Cliente {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.Esperas == nil {
		cfg.Esperas = []time.Duration{200 * time.Millisecond, 400 * time.Millisecond}
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{}
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return &Cliente{cfg: cfg}
}

type retencionDTO struct {
	ID               string          `json:"id"`
	OfertaID         string          `json:"ofertaId"`
	Cantidad         int             `json:"cantidad"`
	Descripcion      string          `json:"descripcion"`
	Destino          string          `json:"destino"`
	PrecioReferencia decimal.Decimal `json:"precioReferencia"`
}

type grupoDTO struct {
	GrupoID     string         `json:"grupoId"`
	ExpiraEn    time.Time      `json:"expiraEn"`
	Retenciones []retencionDTO `json:"retenciones"`
}

type lineaDTO struct {
	RetencionID string          `json:"retencionId"`
	OfertaID    string          `json:"ofertaId"`
	Descripcion string          `json:"descripcion"`
	Destino     string          `json:"destino"`
	Precio      decimal.Decimal `json:"precio"`
}

type confirmacionDTO struct {
	CompraID string     `json:"compraId"`
	Lineas   []lineaDTO `json:"lineas"`
}

type problemaDTO struct {
	Code              string     `json:"code"`
	Detail            string     `json:"detail"`
	OfertaID          string     `json:"ofertaId"`
	ProximaLiberacion *time.Time `json:"proximaLiberacion"`
	RetencionIDs      []string   `json:"retencionIds"`
}

func (c *Cliente) Retener(ctx context.Context, s ports.SolicitudRetencion) (ports.GrupoRetenido, error) {
	cuerpo := map[string]any{"titularId": s.TitularID, "cantidad": s.Cantidad}
	if s.PackID != "" {
		cuerpo["packId"] = s.PackID
		cuerpo["opciones"] = s.Opciones
	} else {
		cuerpo["ofertaId"] = s.OfertaID
	}
	var g grupoDTO
	estado, prob, err := c.llamar(ctx, http.MethodPost, "/internal/retenciones", s.IdempotencyKey, cuerpo, &g, http.StatusCreated)
	if err != nil {
		return ports.GrupoRetenido{}, err
	}
	switch estado {
	case http.StatusCreated:
		res := ports.GrupoRetenido{ExpiraEn: g.ExpiraEn}
		for _, r := range g.Retenciones {
			res.Retenciones = append(res.Retenciones, ports.Retencion{
				ID: r.ID, OfertaID: r.OfertaID, Cantidad: r.Cantidad,
				Descripcion: r.Descripcion, Destino: r.Destino, PrecioReferencia: r.PrecioReferencia,
			})
		}
		return res, nil
	case http.StatusConflict:
		return ports.GrupoRetenido{}, &ports.ErrSinCupo{Estado: prob.Code, OfertaID: prob.OfertaID, ProximaLiberacion: prob.ProximaLiberacion}
	default: // 404 y 422
		return ports.GrupoRetenido{}, &ports.ErrReglaCatalogo{Codigo: prob.Code, Detalle: prob.Detail}
	}
}

func (c *Cliente) Liberar(ctx context.Context, retencionID string) error {
	_, _, err := c.llamar(ctx, http.MethodDelete, "/internal/retenciones/"+url.PathEscape(retencionID), "", nil, nil, http.StatusNoContent)
	return err
}

func (c *Cliente) Confirmar(ctx context.Context, compraID, titularID string, retenciones []string) ([]domain.LineaCompra, error) {
	cuerpo := map[string]any{"compraId": compraID, "titularId": titularID, "retencionIds": retenciones}
	var conf confirmacionDTO
	estado, prob, err := c.llamar(ctx, http.MethodPost, "/internal/retenciones/confirmacion", compraID, cuerpo, &conf, http.StatusOK)
	if err != nil {
		return nil, err
	}
	if estado == http.StatusConflict {
		return nil, &ports.ErrRetencionesNoVigentes{Retenciones: prob.RetencionIDs}
	}
	if estado != http.StatusOK {
		return nil, fmt.Errorf("%w: confirmación respondió %d %s", ports.ErrCatalogoNoDisponible, estado, prob.Code)
	}
	lineas := make([]domain.LineaCompra, len(conf.Lineas))
	for i, l := range conf.Lineas {
		lineas[i] = domain.LineaCompra{RetencionID: l.RetencionID, OfertaID: l.OfertaID, Descripcion: l.Descripcion, Destino: l.Destino, Precio: l.Precio}
	}
	return lineas, nil
}

// llamar hace la solicitud con reintentos. Devuelve el estado de la última
// respuesta concluyente: la esperada (decodificada en destino) o un 4xx de
// negocio (en el problema). Timeout, error de red o 5xx después de los
// reintentos devuelven ErrCatalogoNoDisponible.
func (c *Cliente) llamar(ctx context.Context, metodo, ruta, clave string, cuerpo, destino any, esperado int) (int, problemaDTO, error) {
	var datos []byte
	if cuerpo != nil {
		var err error
		if datos, err = json.Marshal(cuerpo); err != nil {
			return 0, problemaDTO{}, err
		}
	}
	var ultimo error
	for intento := 0; ; intento++ {
		estado, prob, err := c.intentar(ctx, metodo, ruta, clave, datos, destino, esperado)
		if err == nil {
			return estado, prob, nil
		}
		ultimo = err
		if intento >= len(c.cfg.Esperas) || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(conJitter(c.cfg.Esperas[intento])):
			continue
		}
		break
	}
	return 0, problemaDTO{}, fmt.Errorf("%w: %v", ports.ErrCatalogoNoDisponible, ultimo)
}

// intentar devuelve error solo si hay que reintentar (red, timeout o 5xx).
func (c *Cliente) intentar(ctx context.Context, metodo, ruta, clave string, datos []byte, destino any, esperado int) (int, problemaDTO, error) {
	ctx, cancelar := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancelar()
	var cuerpo io.Reader
	if datos != nil {
		cuerpo = bytes.NewReader(datos)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, c.cfg.URL+ruta, cuerpo)
	if err != nil {
		return 0, problemaDTO{}, err
	}
	if datos != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if clave != "" {
		req.Header.Set("Idempotency-Key", clave)
	}
	if id := observabilidad.Correlacion(ctx); id != "" {
		req.Header.Set("X-Correlation-Id", id)
	}

	res, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return 0, problemaDTO{}, err
	}
	defer res.Body.Close()
	leido, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return 0, problemaDTO{}, err
	}

	switch {
	case res.StatusCode == esperado:
		if destino != nil {
			if err := json.Unmarshal(leido, destino); err != nil {
				return 0, problemaDTO{}, fmt.Errorf("respuesta inválida de Catálogo: %w", err)
			}
		}
		return res.StatusCode, problemaDTO{}, nil
	case res.StatusCode >= 500:
		return 0, problemaDTO{}, fmt.Errorf("catálogo respondió %d", res.StatusCode)
	case res.StatusCode >= 400:
		// Errores de negocio (404, 409, 422): no se reintentan.
		var p problemaDTO
		_ = json.Unmarshal(leido, &p)
		return res.StatusCode, p, nil
	default:
		return 0, problemaDTO{}, errors.New("catálogo respondió un estado inesperado: " + res.Status)
	}
}

func conJitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}
