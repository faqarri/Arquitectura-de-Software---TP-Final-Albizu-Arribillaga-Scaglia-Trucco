// Package smtp implementa ports.Mailer. En local apunta a Mailpit.
package smtp

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/google/uuid"

	"agencia/comprobantes/internal/ports"
)

// TiempoMaximoEnvio acota cada envío: si el servidor no termina de aceptar el
// mensaje en ese plazo, Enviar devuelve error y el worker lo reintenta.
const TiempoMaximoEnvio = 10 * time.Second

const nombreRemitente = "Agencia de Viajes"

// Config del servidor de correo. Sin autenticación ni TLS: en local es Mailpit.
type Config struct {
	Host      string
	Puerto    string
	Remitente string
}

type Mailer struct {
	cfg     Config
	timeout time.Duration
}

func NuevoMailer(cfg Config) *Mailer { return &Mailer{cfg: cfg, timeout: TiempoMaximoEnvio} }

var _ ports.Mailer = (*Mailer)(nil)

// Enviar arma un mensaje MIME multipart (cuerpo en texto y PDF adjunto) y lo
// entrega por SMTP. Devuelve error si el servidor no aceptó el mensaje, si ctx
// se cancela o si pasa TiempoMaximoEnvio.
func (m *Mailer) Enviar(ctx context.Context, correo ports.Correo) error {
	ctx, cancelar := context.WithTimeout(ctx, m.timeout)
	defer cancelar()

	mensaje, err := armarMensaje(m.cfg.Remitente, correo, time.Now())
	if err != nil {
		return err
	}
	if err := m.entregar(ctx, correo.Para, mensaje); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("enviar correo a %s: %w (%v)", correo.Para, ctx.Err(), err)
		}
		return fmt.Errorf("enviar correo a %s: %w", correo.Para, err)
	}
	return nil
}

// entregar habla SMTP con el servidor. net/smtp no recibe contexto: el plazo se
// aplica con el deadline de la conexión, que también se vence si ctx se cancela.
func (m *Mailer) entregar(ctx context.Context, para string, mensaje []byte) error {
	direccion := net.JoinHostPort(m.cfg.Host, m.cfg.Puerto)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", direccion)
	if err != nil {
		return fmt.Errorf("conectar con %s: %w", direccion, err)
	}
	defer conn.Close()
	if limite, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(limite)
	}
	detener := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer detener()

	cliente, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return fmt.Errorf("saludo SMTP: %w", err)
	}
	defer cliente.Close()
	if err := cliente.Mail(m.cfg.Remitente); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := cliente.Rcpt(para); err != nil {
		return fmt.Errorf("RCPT TO: %w", err)
	}
	w, err := cliente.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(mensaje); err != nil {
		return fmt.Errorf("escribir el mensaje: %w", err)
	}
	// Al cerrar DATA el servidor responde si aceptó el mensaje.
	if err := w.Close(); err != nil {
		return fmt.Errorf("el servidor no aceptó el mensaje: %w", err)
	}
	// El mensaje ya fue aceptado: un error en QUIT no cambia el resultado.
	_ = cliente.Quit()
	return nil
}

// armarMensaje arma el correo en formato MIME: multipart/mixed con una parte de
// texto (quoted-printable, UTF-8) y el adjunto en base64.
func armarMensaje(remitente string, c ports.Correo, ahora time.Time) ([]byte, error) {
	for _, v := range []string{remitente, c.Para, c.Asunto, c.Adjunto.Nombre} {
		if strings.ContainsAny(v, "\r\n") {
			return nil, errors.New("encabezado de correo inválido: contiene saltos de línea")
		}
	}
	if _, err := mail.ParseAddress(c.Para); err != nil {
		return nil, fmt.Errorf("destinatario inválido %q: %w", c.Para, err)
	}

	var partes bytes.Buffer
	mw := multipart.NewWriter(&partes)

	texto, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return nil, err
	}
	qp := quotedprintable.NewWriter(texto)
	if _, err := qp.Write([]byte(c.Cuerpo)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}

	adjunto, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {mime.FormatMediaType("application/pdf", map[string]string{"name": c.Adjunto.Nombre})},
		"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": c.Adjunto.Nombre})},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return nil, err
	}
	if err := escribirBase64(adjunto, c.Adjunto.Contenido); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	dominio := "localhost"
	if _, d, ok := strings.Cut(remitente, "@"); ok && d != "" {
		dominio = d
	}
	var b bytes.Buffer
	encabezados := []struct{ nombre, valor string }{
		{"From", (&mail.Address{Name: nombreRemitente, Address: remitente}).String()},
		{"To", c.Para},
		{"Subject", mime.QEncoding.Encode("utf-8", c.Asunto)},
		{"Date", ahora.Format(time.RFC1123Z)},
		{"Message-ID", "<" + uuid.NewString() + "@" + dominio + ">"},
		{"MIME-Version", "1.0"},
		{"Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": mw.Boundary()})},
	}
	for _, e := range encabezados {
		fmt.Fprintf(&b, "%s: %s\r\n", e.nombre, e.valor)
	}
	b.WriteString("\r\n")
	b.Write(partes.Bytes())
	return b.Bytes(), nil
}

// escribirBase64 escribe datos en base64 con líneas de 76 caracteres (RFC 2045).
func escribirBase64(w interface{ Write([]byte) (int, error) }, datos []byte) error {
	codificado := base64.StdEncoding.EncodeToString(datos)
	for len(codificado) > 0 {
		n := min(76, len(codificado))
		if _, err := fmt.Fprintf(w, "%s\r\n", codificado[:n]); err != nil {
			return err
		}
		codificado = codificado[n:]
	}
	return nil
}
