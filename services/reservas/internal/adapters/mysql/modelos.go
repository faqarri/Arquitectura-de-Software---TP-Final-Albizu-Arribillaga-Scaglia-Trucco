package mysql

import (
	"time"

	"github.com/shopspring/decimal"

	"agencia/reservas/internal/domain"
)

// Modelos de GORM: la forma de las tablas. El dominio no conoce GORM.

type itemModelo struct {
	ID               string                `gorm:"type:char(36);primaryKey"`
	UsuarioID        string                `gorm:"size:64;not null;uniqueIndex:ux_item_usuario_clave,priority:1"`
	IdempotencyKey   string                `gorm:"size:64;not null;uniqueIndex:ux_item_usuario_clave,priority:2"`
	OfertaID         string                `gorm:"size:64"`
	PackID           string                `gorm:"size:64"`
	Cantidad         int                   `gorm:"not null"`
	Descripcion      string                `gorm:"size:1000;not null"`
	Destino          string                `gorm:"size:120"`
	PrecioReferencia decimal.Decimal       `gorm:"type:decimal(12,2);not null"`
	ExpiraEn         time.Time             `gorm:"type:datetime(3);not null"`
	Estado           string                `gorm:"size:16;not null"`
	CreadoEn         time.Time             `gorm:"type:datetime(3);not null"`
	Retenciones      []itemRetencionModelo `gorm:"foreignKey:ItemID;constraint:OnDelete:CASCADE"`
}

func (itemModelo) TableName() string { return "items_carrito" }

// itemRetencionModelo permite encontrar el ítem de una retención (evento RetencionVencida).
type itemRetencionModelo struct {
	RetencionID string `gorm:"size:64;primaryKey"`
	ItemID      string `gorm:"type:char(36);not null;index"`
	Orden       int    `gorm:"not null"`
}

func (itemRetencionModelo) TableName() string { return "item_retenciones" }

type compraModelo struct {
	ID                  string          `gorm:"type:char(36);primaryKey"`
	UsuarioID           string          `gorm:"size:64;not null;uniqueIndex:ux_compra_usuario_clave,priority:1"`
	IdempotencyKey      string          `gorm:"size:64;not null;uniqueIndex:ux_compra_usuario_clave,priority:2"`
	Email               string          `gorm:"size:254;not null"`
	Estado              string          `gorm:"size:16;not null;index:ix_compra_estado_creada,priority:1"`
	Total               decimal.Decimal `gorm:"type:decimal(12,2);not null"`
	Retenciones         []string        `gorm:"type:json;serializer:json;not null"`
	RetencionesVencidas []string        `gorm:"type:json;serializer:json"`
	CreadaEn            time.Time       `gorm:"type:datetime(3);not null;index:ix_compra_estado_creada,priority:2"`
	ActualizadaEn       time.Time       `gorm:"type:datetime(3);not null"`
	Lineas              []lineaModelo   `gorm:"foreignKey:CompraID;constraint:OnDelete:CASCADE"`
}

func (compraModelo) TableName() string { return "compras" }

type lineaModelo struct {
	CompraID    string          `gorm:"type:char(36);primaryKey"`
	Orden       int             `gorm:"primaryKey;autoIncrement:false"`
	RetencionID string          `gorm:"size:64;not null"`
	OfertaID    string          `gorm:"size:64"`
	Descripcion string          `gorm:"size:1000;not null"`
	Destino     string          `gorm:"size:120"`
	Precio      decimal.Decimal `gorm:"type:decimal(12,2);not null"`
}

func (lineaModelo) TableName() string { return "lineas_compra" }

// outboxModelo: eventos escritos en la misma transacción que el cambio que los
// origina (ADR-005). Headers guarda la correlación y la traza de la solicitud.
type outboxModelo struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement"`
	Tipo        string     `gorm:"size:64;not null"`
	Payload     string     `gorm:"type:json;not null"`
	Headers     string     `gorm:"type:json;not null"`
	CreadoEn    time.Time  `gorm:"autoCreateTime"`
	PublicadoEn *time.Time `gorm:"index"`
}

func (outboxModelo) TableName() string { return "outbox" }

type guiaModelo struct {
	ID             string    `gorm:"type:char(36);primaryKey"`
	UsuarioID      string    `gorm:"size:64;not null;index"`
	IdempotencyKey string    `gorm:"size:64;not null;uniqueIndex"`
	Titulo         string    `gorm:"size:150;not null"`
	Descripcion    string    `gorm:"type:text"`
	Destino        string    `gorm:"size:120;not null"`
	Etiquetas      []string  `gorm:"type:json;serializer:json"`
	Autor          string    `gorm:"size:100;not null"`
	DocumentoID    string    `gorm:"size:64"`
	Estado         string    `gorm:"size:16;not null;index"`
	MotivoRechazo  string    `gorm:"size:2000"`
	Intentos       int       `gorm:"not null;default:0"`
	CreadaEn       time.Time `gorm:"type:datetime(3);not null"`
	ActualizadaEn  time.Time `gorm:"type:datetime(3);not null"`
}

func (guiaModelo) TableName() string { return "guias" }

// archivoGuiaModelo guarda el PDF hasta que Estudiario acepta la guía; después se borra.
type archivoGuiaModelo struct {
	GuiaID string `gorm:"type:char(36);primaryKey"`
	PDF    []byte `gorm:"type:mediumblob;not null"`
}

func (archivoGuiaModelo) TableName() string { return "guias_archivos" }

// appLogModelo es la tabla donde pkg/observabilidad persiste los logs (ADR-011).
type appLogModelo struct {
	ID            uint64    `gorm:"primaryKey;autoIncrement"`
	Fecha         time.Time `gorm:"type:datetime(6);not null;index:idx_app_logs_fecha"`
	Nivel         string    `gorm:"size:16;not null"`
	Servicio      string    `gorm:"size:100;not null"`
	Mensaje       string    `gorm:"type:text;not null"`
	CorrelacionID *string   `gorm:"size:64;index:idx_app_logs_correlacion"`
	TraceID       *string   `gorm:"type:char(32);index:idx_app_logs_trace"`
	Metadatos     string    `gorm:"type:json;not null"`
}

func (appLogModelo) TableName() string { return "app_logs" }

// ---------- conversiones ----------

func itemAModelo(it domain.ItemCarrito) itemModelo {
	m := itemModelo{
		ID: it.ID, UsuarioID: it.UsuarioID, IdempotencyKey: it.IdempotencyKey, OfertaID: it.OfertaID,
		PackID: it.PackID, Cantidad: it.Cantidad, Descripcion: it.Descripcion, Destino: it.Destino,
		PrecioReferencia: it.PrecioReferencia, ExpiraEn: it.ExpiraEn, Estado: string(it.Estado), CreadoEn: it.CreadoEn,
	}
	for i, r := range it.Retenciones {
		m.Retenciones = append(m.Retenciones, itemRetencionModelo{RetencionID: r, ItemID: it.ID, Orden: i})
	}
	return m
}

func itemADominio(m itemModelo) domain.ItemCarrito {
	it := domain.ItemCarrito{
		ID: m.ID, UsuarioID: m.UsuarioID, IdempotencyKey: m.IdempotencyKey, OfertaID: m.OfertaID,
		PackID: m.PackID, Cantidad: m.Cantidad, Descripcion: m.Descripcion, Destino: m.Destino,
		PrecioReferencia: m.PrecioReferencia, ExpiraEn: m.ExpiraEn.UTC(), Estado: domain.EstadoItem(m.Estado), CreadoEn: m.CreadoEn.UTC(),
	}
	for _, r := range m.Retenciones {
		it.Retenciones = append(it.Retenciones, r.RetencionID)
	}
	return it
}

func compraAModelo(c *domain.Compra) compraModelo {
	m := compraModelo{
		ID: c.ID, UsuarioID: c.UsuarioID, IdempotencyKey: c.IdempotencyKey, Email: c.Email,
		Estado: string(c.Estado), Total: c.Total, Retenciones: c.Retenciones, RetencionesVencidas: c.RetencionesVencidas,
		CreadaEn: c.CreadaEn, ActualizadaEn: c.ActualizadaEn,
	}
	if m.Retenciones == nil {
		m.Retenciones = []string{}
	}
	return m
}

func compraADominio(m compraModelo) domain.Compra {
	c := domain.Compra{
		ID: m.ID, UsuarioID: m.UsuarioID, IdempotencyKey: m.IdempotencyKey, Email: m.Email,
		Estado: domain.EstadoCompra(m.Estado), Total: m.Total, Retenciones: m.Retenciones,
		RetencionesVencidas: m.RetencionesVencidas, CreadaEn: m.CreadaEn.UTC(), ActualizadaEn: m.ActualizadaEn.UTC(),
	}
	for _, l := range m.Lineas {
		c.Lineas = append(c.Lineas, domain.LineaCompra{RetencionID: l.RetencionID, OfertaID: l.OfertaID, Descripcion: l.Descripcion, Destino: l.Destino, Precio: l.Precio})
	}
	return c
}

func guiaAModelo(g *domain.Guia) guiaModelo {
	return guiaModelo{
		ID: g.ID, UsuarioID: g.UsuarioID, IdempotencyKey: g.IdempotencyKey, Titulo: g.Titulo, Descripcion: g.Descripcion,
		Destino: g.Destino, Etiquetas: g.Etiquetas, Autor: g.Autor, DocumentoID: g.DocumentoID, Estado: string(g.Estado),
		MotivoRechazo: g.MotivoRechazo, Intentos: g.Intentos, CreadaEn: g.CreadaEn, ActualizadaEn: g.ActualizadaEn,
	}
}

func guiaADominio(m guiaModelo) domain.Guia {
	return domain.Guia{
		ID: m.ID, UsuarioID: m.UsuarioID, IdempotencyKey: m.IdempotencyKey, Titulo: m.Titulo, Descripcion: m.Descripcion,
		Destino: m.Destino, Etiquetas: m.Etiquetas, Autor: m.Autor, DocumentoID: m.DocumentoID, Estado: domain.EstadoGuia(m.Estado),
		MotivoRechazo: m.MotivoRechazo, Intentos: m.Intentos, CreadaEn: m.CreadaEn.UTC(), ActualizadaEn: m.ActualizadaEn.UTC(),
	}
}
