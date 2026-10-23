// Package smtp implementa ports.Mailer. En local apunta a Mailpit.
package smtp

import (
	"context"
	"errors"

	"agencia/comprobantes/internal/ports"
)

// Config del servidor de correo.
type Config struct {
	Host      string
	Puerto    string
	Remitente string
}

type Mailer struct {
	cfg Config
}

func NuevoMailer(cfg Config) *Mailer { return &Mailer{cfg: cfg} }

var _ ports.Mailer = (*Mailer)(nil)

// Enviar: TODO(rabbit). Armar un mensaje MIME multipart con el cuerpo y el PDF
// adjunto y enviarlo con net/smtp. Respetar ctx (timeout) para no colgar al worker.
func (m *Mailer) Enviar(ctx context.Context, correo ports.Correo) error {
	return errors.New("no implementado")
}
