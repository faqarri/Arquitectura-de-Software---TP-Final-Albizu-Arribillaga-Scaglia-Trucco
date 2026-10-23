package mysql

import (
	"time"

	"github.com/shopspring/decimal"
)

// Modelos de GORM: la forma de las tablas. Son propios del adaptador; el dominio
// no conoce GORM. Esquema documentado en services/comprobantes/README.md.

type comprobanteModelo struct {
	ID             string          `gorm:"type:char(36);primaryKey"`
	Nro            int64           `gorm:"autoIncrement;uniqueIndex;not null"`
	Consumidor     string          `gorm:"size:64;not null;uniqueIndex:ux_consumidor_clave,priority:1"`
	IdempotencyKey string          `gorm:"size:64;not null;uniqueIndex:ux_consumidor_clave,priority:2"`
	HashSolicitud  string          `gorm:"type:char(64);not null"`
	Email          string          `gorm:"size:254;not null"`
	Total          decimal.Decimal `gorm:"type:decimal(12,2);not null"`
	EstadoEnvio    string          `gorm:"size:16;not null;index"`
	IntentosEnvio  int             `gorm:"not null;default:0"`
	FechaEmision   time.Time       `gorm:"not null"`
	ActualizadoEn  time.Time       `gorm:"autoUpdateTime"`
	Items          []itemModelo    `gorm:"foreignKey:ComprobanteID;constraint:OnDelete:CASCADE"`
}

func (comprobanteModelo) TableName() string { return "comprobantes" }

type itemModelo struct {
	ComprobanteID string          `gorm:"type:char(36);primaryKey"`
	Orden         int             `gorm:"primaryKey;autoIncrement:false"`
	Descripcion   string          `gorm:"size:200;not null"`
	Precio        decimal.Decimal `gorm:"type:decimal(12,2);not null"`
}

func (itemModelo) TableName() string { return "items_comprobante" }

// outboxModelo guarda los eventos a publicar en RabbitMQ, escritos en la misma
// transacción que el cambio que los origina (ADR-005). Headers guarda la
// correlación y la traza de la solicitud original, porque el evento se publica
// después, desde otra goroutine, cuando ese contexto ya no existe.
type outboxModelo struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement"`
	Tipo        string     `gorm:"size:64;not null"`
	Payload     string     `gorm:"type:json;not null"`
	Headers     string     `gorm:"type:json;not null"`
	CreadoEn    time.Time  `gorm:"autoCreateTime"`
	PublicadoEn *time.Time `gorm:"index"`
}

func (outboxModelo) TableName() string { return "outbox" }
