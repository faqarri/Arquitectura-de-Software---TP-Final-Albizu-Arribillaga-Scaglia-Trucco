package jobs

import (
	"context"
	"log/slog"
	"time"
)

// PreparadorBusqueda es lo que se usa de services.Busqueda.
type PreparadorBusqueda interface {
	Preparar(ctx context.Context) error
}

// PrepararBusqueda crea y carga el índice de búsqueda si no existe. Corre en
// segundo plano para que Catálogo arranque aunque OpenSearch tarde en levantar
// (suele tardar más que el resto): reintenta con espera creciente hasta lograrlo
// o hasta que ctx se cancela. Mientras tanto, la búsqueda responde 503 y el
// indexador no arranca: si arrancara, agotaría los reintentos de los mensajes
// contra un índice que todavía no existe y los mandaría a la DLQ.
func PrepararBusqueda(ctx context.Context, log *slog.Logger, p PreparadorBusqueda) error {
	log = log.With("componente", "preparación de la búsqueda")
	espera := 2 * time.Second
	for {
		err := p.Preparar(ctx)
		if err == nil {
			log.Info("índice de búsqueda listo")
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Warn("OpenSearch todavía no está listo; se reintenta", "error", err, "espera", espera.String())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(espera):
		}
		espera = min(espera*2, 30*time.Second)
	}
}
