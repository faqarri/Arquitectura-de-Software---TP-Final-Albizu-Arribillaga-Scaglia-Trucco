package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"agencia/pkg/mensajeria"
	"agencia/pkg/observabilidad"
)

// Eventos que Catálogo publica por el outbox de MySQL en el exchange fanout
// catalogo.disponibilidad (ADR-015, docs/contracts/internos/README.md).
const (
	TipoDisponibilidadCambiada = "DisponibilidadCambiada"
	TipoRetencionVencida       = "RetencionVencida"
)

type eventoDisponibilidad struct {
	EventID           string     `json:"eventId"`
	Tipo              string     `json:"tipo"`
	OfertaID          string     `json:"ofertaId"`
	Estado            string     `json:"estado"`
	Libres            int        `json:"libres"`
	ProximaLiberacion *time.Time `json:"proximaLiberacion"`
	Version           int64      `json:"version"`
}

type eventoRetencionVencida struct {
	EventID     string    `json:"eventId"`
	Tipo        string    `json:"tipo"`
	RetencionID string    `json:"retencionId"`
	OfertaID    string    `json:"ofertaId"`
	TitularID   string    `json:"titularId"`
	ExpiraEn    time.Time `json:"expiraEn"`
}

// headersDe arma los headers del mensaje con la correlación y la traza de la
// operación, para seguirla en los logs y en Jaeger.
func headersDe(ctx context.Context) string {
	h := map[string]any{}
	observabilidad.InyectarEnMensaje(ctx, h)
	crudo, err := json.Marshal(h)
	if err != nil {
		return "{}"
	}
	return string(crudo)
}

// insertarOutbox registra el evento en la misma transacción que el cambio que
// lo origina: o se guardan los dos, o ninguno.
func insertarOutbox(ctx context.Context, tx *sql.Tx, tipo string, evento any, headers string) error {
	payload, err := json.Marshal(evento)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox (tipo, payload, headers) VALUES (?, ?, ?)`, tipo, payload, headers); err != nil {
		return fmt.Errorf("registrar evento en el outbox: %w", err)
	}
	return nil
}

// Outbox es lo que lee el publicador de pkg/mensajeria.
type Outbox struct {
	db *sql.DB
}

var _ mensajeria.Outbox = (*Outbox)(nil)

func NuevoOutbox(db *sql.DB) *Outbox { return &Outbox{db: db} }

func (o *Outbox) Pendientes(ctx context.Context, limite int) ([]mensajeria.EventoOutbox, error) {
	filas, err := o.db.QueryContext(ctx,
		`SELECT id, tipo, payload, headers FROM outbox WHERE publicado_en IS NULL ORDER BY id LIMIT ?`, limite)
	if err != nil {
		return nil, fmt.Errorf("leer outbox: %w", err)
	}
	defer filas.Close()
	var eventos []mensajeria.EventoOutbox
	for filas.Next() {
		var ev mensajeria.EventoOutbox
		var headers []byte
		if err := filas.Scan(&ev.ID, &ev.Tipo, &ev.Payload, &headers); err != nil {
			return nil, err
		}
		ev.Headers = map[string]any{}
		if err := json.Unmarshal(headers, &ev.Headers); err != nil {
			return nil, fmt.Errorf("evento %d del outbox con headers inválidos: %w", ev.ID, err)
		}
		eventos = append(eventos, ev)
	}
	return eventos, filas.Err()
}

func (o *Outbox) MarcarPublicado(ctx context.Context, id uint64) error {
	_, err := o.db.ExecContext(ctx, `UPDATE outbox SET publicado_en = NOW(6) WHERE id = ?`, id)
	return err
}
