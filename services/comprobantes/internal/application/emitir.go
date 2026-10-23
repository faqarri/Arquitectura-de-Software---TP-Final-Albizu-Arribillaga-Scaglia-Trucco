// Package application contiene los casos de uso de Comprobantes. Depende solo
// de domain y de ports (ADR-002).
package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
)

// ErrConflictoIdempotencia: la clave ya se usó con un cuerpo distinto (409).
var ErrConflictoIdempotencia = errors.New("la clave de idempotencia ya se usó con otra solicitud")

// SolicitudEmision es lo que llega por la API pública o por CompraConfirmada.
// Los precios vienen tal como los envió el consumidor, sin redondear.
type SolicitudEmision struct {
	Consumidor     string
	IdempotencyKey string
	Email          string
	Items          []domain.Item
}

// ResultadoEmision indica si el comprobante se creó ahora (201) o ya existía
// por un reintento con la misma clave y el mismo cuerpo (200).
type ResultadoEmision struct {
	Comprobante *domain.Comprobante
	YaExistia   bool
}

// EmitirComprobante es el caso de uso compartido por la API y el consumidor de
// CompraConfirmada: emite el comprobante y deja pedido su envío por correo.
type EmitirComprobante struct {
	repo  ports.RepositorioComprobantes
	ahora func() time.Time
}

func NuevoEmitirComprobante(repo ports.RepositorioComprobantes) *EmitirComprobante {
	return &EmitirComprobante{repo: repo, ahora: time.Now}
}

func (uc *EmitirComprobante) Ejecutar(ctx context.Context, s SolicitudEmision) (ResultadoEmision, error) {
	hash := HashSolicitud(s.Email, s.Items)

	c, err := domain.NuevoComprobante(s.Consumidor, s.IdempotencyKey, hash, s.Email, s.Items, uc.ahora())
	if err != nil {
		return ResultadoEmision{}, err
	}

	err = uc.repo.Guardar(ctx, c)
	if err == nil {
		return ResultadoEmision{Comprobante: c}, nil
	}
	if !errors.Is(err, ports.ErrClaveDuplicada) {
		return ResultadoEmision{}, err
	}

	existente, err := uc.repo.BuscarPorClave(ctx, s.Consumidor, s.IdempotencyKey)
	if err != nil {
		return ResultadoEmision{}, err
	}
	if existente.HashSolicitud != hash {
		return ResultadoEmision{}, ErrConflictoIdempotencia
	}
	return ResultadoEmision{Comprobante: existente, YaExistia: true}, nil
}

// HashSolicitud es el SHA-256 del cuerpo normalizado, calculado con los precios
// originales (antes de redondear). El JSON se vuelve a serializar con un orden de
// campos fijo, así espacios u orden distinto en el cuerpo recibido no cambian el hash.
func HashSolicitud(email string, items []domain.Item) string {
	type itemNormalizado struct {
		Descripcion string `json:"descripcion"`
		Precio      string `json:"precio"`
	}
	normalizado := struct {
		Email string            `json:"email"`
		Items []itemNormalizado `json:"items"`
	}{Email: email, Items: make([]itemNormalizado, len(items))}
	for i, it := range items {
		normalizado.Items[i] = itemNormalizado{Descripcion: it.Descripcion, Precio: it.Precio.String()}
	}
	b, _ := json.Marshal(normalizado) // no puede fallar: solo contiene strings
	suma := sha256.Sum256(b)
	return hex.EncodeToString(suma[:])
}
