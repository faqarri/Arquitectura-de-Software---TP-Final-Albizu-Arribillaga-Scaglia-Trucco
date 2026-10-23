package application

import (
	"context"
	"fmt"

	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
)

// EnviarComprobante genera el PDF y envía el correo de un comprobante emitido.
// Lo ejecuta el worker que consume la cola comprobantes.envios; los reintentos y
// la DLQ son responsabilidad de ese adaptador.
type EnviarComprobante struct {
	repo   ports.RepositorioComprobantes
	pdf    ports.GeneradorPDF
	mailer ports.Mailer
}

func NuevoEnviarComprobante(repo ports.RepositorioComprobantes, pdf ports.GeneradorPDF, mailer ports.Mailer) *EnviarComprobante {
	return &EnviarComprobante{repo: repo, pdf: pdf, mailer: mailer}
}

// Ejecutar envía el correo. Si el comprobante ya no está PENDIENTE no hace nada,
// así una entrega repetida del mensaje no manda un segundo correo.
func (uc *EnviarComprobante) Ejecutar(ctx context.Context, id string) error {
	c, err := uc.repo.Obtener(ctx, id)
	if err != nil {
		return err
	}
	if c.EstadoEnvio != domain.EnvioPendiente {
		return nil
	}

	pdf, err := uc.pdf.Generar(ctx, c)
	if err != nil {
		return fmt.Errorf("generar PDF: %w", err)
	}
	err = uc.mailer.Enviar(ctx, ports.Correo{
		Para:    c.Email,
		Asunto:  "Tu comprobante " + c.Numero(),
		Cuerpo:  fmt.Sprintf("Adjuntamos el comprobante %s por un total de $ %s.", c.Numero(), c.Total.StringFixed(2)),
		Adjunto: ports.Adjunto{Nombre: c.Numero() + ".pdf", Contenido: pdf},
	})
	if err != nil {
		return fmt.Errorf("enviar correo: %w", err)
	}

	if err := c.MarcarEnviado(); err != nil {
		return err
	}
	return uc.repo.ActualizarEnvio(ctx, c)
}

// MarcarFallido se llama cuando se agotaron los reintentos de envío.
func (uc *EnviarComprobante) MarcarFallido(ctx context.Context, id string) error {
	c, err := uc.repo.Obtener(ctx, id)
	if err != nil {
		return err
	}
	if c.EstadoEnvio != domain.EnvioPendiente {
		return nil
	}
	if err := c.MarcarFallido(); err != nil {
		return err
	}
	return uc.repo.ActualizarEnvio(ctx, c)
}
