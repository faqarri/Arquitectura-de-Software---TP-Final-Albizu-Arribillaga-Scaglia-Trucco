package application

import (
	"context"
	"errors"
	"time"

	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

// Guias agrupa los casos de uso de las guías de viaje (HU-13), que se publican
// en Estudiario, el servicio de otro grupo (ADR-009).
type Guias struct {
	repo     ports.RepositorioGuias
	compras  ports.RepositorioCompras
	externas ports.GuiasExternas
	ahora    func() time.Time
	// esperaEnvioInmediato acota el intento de envío dentro de la solicitud del
	// cliente. Sin ese límite, con Estudiario caído los reintentos del registro
	// superan el timeout del gateway (15 s) y el cliente recibe un 502 aunque la
	// guía quedó guardada.
	esperaEnvioInmediato time.Duration
}

// EsperaEnvioInmediato es el máximo que Compartir espera a Estudiario antes de
// responder; lo que no se completa en ese tiempo lo termina EnviarPendientes.
const EsperaEnvioInmediato = 8 * time.Second

func NuevoGuias(repo ports.RepositorioGuias, compras ports.RepositorioCompras, externas ports.GuiasExternas) *Guias {
	return &Guias{repo: repo, compras: compras, externas: externas, ahora: time.Now, esperaEnvioInmediato: EsperaEnvioInmediato}
}

// Compartir guarda la guía (solo de un destino comprado) y trata de enviarla en
// el momento. Si Estudiario no responde, la guía queda POR_ENVIAR y la envía
// después EnviarPendientes: la falla del proveedor no le llega al cliente como error.
func (uc *Guias) Compartir(ctx context.Context, usuarioID string, s domain.SolicitudGuia) (*domain.Guia, error) {
	compras, err := uc.compras.Listar(ctx, usuarioID)
	if err != nil {
		return nil, err
	}
	var destinos []string
	for _, c := range compras {
		destinos = append(destinos, c.Destinos()...)
	}
	g, err := domain.NuevaGuia(usuarioID, s, destinos, uc.ahora())
	if err != nil {
		return nil, err
	}
	if err := uc.repo.Crear(ctx, g, s.PDF); err != nil {
		return nil, err
	}
	_ = uc.enviar(ctx, g, s.PDF, uc.esperaEnvioInmediato) // si falla o tarda, queda POR_ENVIAR
	return g, nil
}

// enviar registra la guía en Estudiario y guarda el resultado. Con limite > 0,
// el registro se corta a ese tiempo; el resultado se guarda igual con ctx. Si el
// corte llega después de que Estudiario la aceptó, el reenvío usa la misma
// Idempotency-Key y no la duplica.
func (uc *Guias) enviar(ctx context.Context, g *domain.Guia, pdf []byte, limite time.Duration) error {
	ctxRegistro := ctx
	if limite > 0 {
		var cancelar context.CancelFunc
		ctxRegistro, cancelar = context.WithTimeout(ctx, limite)
		defer cancelar()
	}
	id, err := uc.externas.Registrar(ctxRegistro, *g, pdf)
	var rechazo *ports.ErrGuiaRechazada
	switch {
	case err == nil:
		g.Registrada(id, uc.ahora())
	case errors.As(err, &rechazo):
		g.Moderada(false, rechazo.Motivo, uc.ahora())
	default:
		g.FalloEnvio(uc.ahora())
	}
	if errAct := uc.repo.Actualizar(ctx, g); errAct != nil {
		return errAct
	}
	if err != nil && rechazo == nil {
		return err
	}
	return nil
}

// EnviarPendientes reintenta el envío de las guías POR_ENVIAR. Se detiene al
// primer error de disponibilidad: si Estudiario está caído, no tiene sentido
// seguir intentando en este ciclo.
func (uc *Guias) EnviarPendientes(ctx context.Context, limite int) (enviadas int, err error) {
	pendientes, err := uc.repo.PorEnviar(ctx, limite)
	if err != nil {
		return 0, err
	}
	for i := range pendientes {
		g := pendientes[i].Guia
		if err := uc.enviar(ctx, &g, pendientes[i].PDF, 0); err != nil {
			return enviadas, err
		}
		enviadas++
	}
	return enviadas, nil
}

// ActualizarModeracion consulta a Estudiario el estado de las guías PENDIENTE
// (no hay notificaciones en su v1: hay que preguntar).
func (uc *Guias) ActualizarModeracion(ctx context.Context) (actualizadas int, err error) {
	pendientes, err := uc.repo.PendientesDeModeracion(ctx)
	if err != nil || len(pendientes) == 0 {
		return 0, err
	}
	estados, err := uc.externas.Moderacion(ctx)
	if err != nil {
		return 0, err
	}
	porID := map[string]ports.EstadoModeracion{}
	for _, e := range estados {
		porID[e.DocumentoID] = e
	}
	for i := range pendientes {
		g := &pendientes[i]
		e, ok := porID[g.DocumentoID]
		if !ok || (!e.Publicada && !e.Rechazada) {
			continue
		}
		g.Moderada(e.Publicada, e.Motivo, uc.ahora())
		if err := uc.repo.Actualizar(ctx, g); err != nil {
			return actualizadas, err
		}
		actualizadas++
	}
	return actualizadas, nil
}

func (uc *Guias) Listar(ctx context.Context, usuarioID string) ([]domain.Guia, error) {
	return uc.repo.Listar(ctx, usuarioID)
}
