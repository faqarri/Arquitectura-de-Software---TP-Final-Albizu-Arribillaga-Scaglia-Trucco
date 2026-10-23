package smtp

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"agencia/comprobantes/internal/ports"
)

// servidorSMTP es un servidor SMTP mínimo para los tests: acepta un mensaje y
// lo deja en recibido.
func servidorSMTP(t *testing.T) (Config, <-chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	recibido := make(chan []byte, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		tp := textproto.NewConn(conn)
		_ = tp.PrintfLine("220 test listo")
		for {
			linea, err := tp.ReadLine()
			if err != nil {
				return
			}
			switch comando := strings.ToUpper(strings.Fields(linea + " x")[0]); comando {
			case "EHLO", "HELO", "MAIL", "RCPT":
				_ = tp.PrintfLine("250 ok")
			case "DATA":
				_ = tp.PrintfLine("354 adelante")
				datos, err := io.ReadAll(tp.DotReader())
				if err != nil {
					return
				}
				recibido <- datos
				_ = tp.PrintfLine("250 aceptado")
			case "QUIT":
				_ = tp.PrintfLine("221 chau")
				return
			default:
				_ = tp.PrintfLine("502 no implementado")
			}
		}
	}()

	host, puerto, _ := net.SplitHostPort(ln.Addr().String())
	return Config{Host: host, Puerto: puerto, Remitente: "comprobantes@agencia-viajes.example"}, recibido
}

func TestEnviarArmaMultipartConElPDFAdjunto(t *testing.T) {
	cfg, recibido := servidorSMTP(t)
	pdf := append([]byte("%PDF-1.3\n"), bytes.Repeat([]byte{0, 1, 2, 250}, 100)...)

	err := NuevoMailer(cfg).Enviar(context.Background(), ports.Correo{
		Para:    "ana.perez@example.com",
		Asunto:  "Tu comprobante B-0001-00000042",
		Cuerpo:  "Adjuntamos el comprobante por un total de $ 31500.00. ¡Gracias!",
		Adjunto: ports.Adjunto{Nombre: "B-0001-00000042.pdf", Contenido: pdf},
	})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := mail.ReadMessage(bytes.NewReader(<-recibido))
	if err != nil {
		t.Fatal(err)
	}
	if got := msg.Header.Get("To"); got != "ana.perez@example.com" {
		t.Errorf("To = %q", got)
	}
	asunto, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if asunto != "Tu comprobante B-0001-00000042" {
		t.Errorf("Subject = %q", asunto)
	}
	tipo, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || tipo != "multipart/mixed" {
		t.Fatalf("Content-Type = %q (%v)", tipo, err)
	}

	mr := multipart.NewReader(msg.Body, params["boundary"])
	texto, err := mr.NextPart() // decodifica quoted-printable
	if err != nil {
		t.Fatal(err)
	}
	cuerpo, _ := io.ReadAll(texto)
	if !strings.Contains(string(cuerpo), "¡Gracias!") {
		t.Errorf("cuerpo = %q", cuerpo)
	}

	adjunto, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if adjunto.FileName() != "B-0001-00000042.pdf" {
		t.Errorf("nombre del adjunto = %q", adjunto.FileName())
	}
	if ct := adjunto.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/pdf") {
		t.Errorf("Content-Type del adjunto = %q", ct)
	}
	contenido, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, adjunto))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contenido, pdf) {
		t.Error("el adjunto recibido no coincide con el PDF enviado")
	}
}

func TestEnviarCortaSiElServidorNoResponde(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		// Acepta la conexión y nunca saluda.
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			time.Sleep(5 * time.Second)
		}
	}()
	host, puerto, _ := net.SplitHostPort(ln.Addr().String())
	m := NuevoMailer(Config{Host: host, Puerto: puerto, Remitente: "a@b.example"})
	m.timeout = 200 * time.Millisecond

	inicio := time.Now()
	err = m.Enviar(context.Background(), ports.Correo{Para: "c@d.example", Asunto: "x", Cuerpo: "x"})
	if err == nil {
		t.Fatal("se esperaba error por timeout")
	}
	if d := time.Since(inicio); d > 2*time.Second {
		t.Errorf("Enviar tardó %v; debía cortar a los 200 ms", d)
	}
}

func TestEnviarFallaSiNoHayServidor(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, puerto, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close() // puerto libre: conexión rechazada, como con Mailpit apagado

	err = NuevoMailer(Config{Host: host, Puerto: puerto, Remitente: "a@b.example"}).
		Enviar(context.Background(), ports.Correo{Para: "c@d.example", Asunto: "x", Cuerpo: "x"})
	if err == nil {
		t.Fatal("se esperaba error")
	}
}

func TestArmarMensajeRechazaSaltosDeLineaEnEncabezados(t *testing.T) {
	_, err := armarMensaje("a@b.example", ports.Correo{Para: "c@d.example\r\nBcc: e@f.example", Asunto: "x"}, time.Now())
	if err == nil {
		t.Fatal("se esperaba error por inyección de encabezados")
	}
}
