// Package jobs reúne los procesos periódicos de Catálogo.
package jobs

import (
	"context"
	"log/slog"
	"time"
)

// Vencedor es lo que se usa de services.Retenciones.
type Vencedor interface {
	VencerPendientes(ctx context.Context) (int, error)
}

// IniciarVencimiento corre el barrido de retenciones vencidas cada intervalo
// (RETENCION_BARRIDO_SEGUNDOS) hasta que ctx se cancela. Si el barrido falla o
// se atrasa no pasa nada grave: la validez de una retención depende de
// expira_en, no de este proceso (RN-11). Los errores se registran una vez por
// error distinto y se reintenta en el próximo ciclo.
func IniciarVencimiento(ctx context.Context, intervalo time.Duration, log *slog.Logger, v Vencedor) error {
	log = log.With("componente", "vencimiento de retenciones")
	tic := time.NewTicker(intervalo)
	defer tic.Stop()
	ultimo := ""
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tic.C:
			ctxBarrido, cancelar := context.WithTimeout(ctx, intervalo)
			n, err := v.VencerPendientes(ctxBarrido)
			cancelar()
			switch {
			case err != nil && err.Error() != ultimo:
				log.Warn("el barrido de vencidas falló; se reintenta en el próximo ciclo", "error", err)
				ultimo = err.Error()
			case err == nil && ultimo != "":
				log.Info("el barrido de vencidas se recuperó")
				ultimo = ""
			}
			if n > 0 {
				log.Info("retenciones vencidas", "cantidad", n)
			}
		}
	}
}
