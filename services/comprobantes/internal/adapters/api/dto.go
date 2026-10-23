package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"agencia/comprobantes/internal/domain"
)

// Límites del contrato (docs/contracts/comprobantes/v1/openapi.yaml).
const (
	maxEmail          = 254
	minItems          = 1
	maxItems          = 100
	maxDescripcion    = 200
	minIdempotencyKey = 8
	maxIdempotencyKey = 64
	maxCuerpo         = 1 << 20 // 1 MiB
)

// errorCampo es un elemento de "errores" en la respuesta 400.
type errorCampo struct {
	Campo   string `json:"campo"`
	Mensaje string `json:"mensaje"`
}

// solicitudEntrada es el cuerpo de POST /v1/comprobantes. Los campos son punteros
// o RawMessage para distinguir "no vino" de "vino vacío" y validar tipos a mano.
type solicitudEntrada struct {
	Email *string        `json:"email"`
	Items *[]itemEntrada `json:"items"`
}

type itemEntrada struct {
	Descripcion *string         `json:"descripcion"`
	Precio      json.RawMessage `json:"precio"`
}

// decodificarSolicitud lee el cuerpo con las reglas del contrato: sin campos
// adicionales, precios numéricos (no strings) leídos sin pasar por float64.
// Devuelve los ítems listos para el caso de uso o la lista de errores.
func decodificarSolicitud(cuerpo io.Reader) (string, []domain.Item, []errorCampo) {
	dec := json.NewDecoder(cuerpo)
	dec.DisallowUnknownFields()

	var s solicitudEntrada
	if err := dec.Decode(&s); err != nil {
		return "", nil, []errorCampo{errorDeDecodificacion(err)}
	}
	if dec.More() {
		return "", nil, []errorCampo{{Campo: "", Mensaje: "el cuerpo debe ser un único objeto JSON"}}
	}

	var errores []errorCampo
	agregar := func(campo, formato string, args ...any) {
		errores = append(errores, errorCampo{Campo: campo, Mensaje: fmt.Sprintf(formato, args...)})
	}

	email := ""
	switch {
	case s.Email == nil:
		agregar("email", "es obligatorio")
	case !emailValido(*s.Email):
		agregar("email", "no es una dirección de correo válida")
	case len(*s.Email) > maxEmail:
		agregar("email", "admite hasta %d caracteres", maxEmail)
	default:
		email = *s.Email
	}

	var items []domain.Item
	switch {
	case s.Items == nil:
		agregar("items", "es obligatorio")
	case len(*s.Items) < minItems || len(*s.Items) > maxItems:
		agregar("items", "debe tener entre %d y %d ítems", minItems, maxItems)
	default:
		for i, it := range *s.Items {
			campo := fmt.Sprintf("items[%d]", i)
			if it.Descripcion == nil {
				agregar(campo+".descripcion", "es obligatorio")
			} else if n := utf8.RuneCountInString(*it.Descripcion); n < 1 || n > maxDescripcion {
				agregar(campo+".descripcion", "debe tener entre 1 y %d caracteres", maxDescripcion)
			}
			precio, err := leerPrecio(it.Precio)
			if err != nil {
				agregar(campo+".precio", "%s", err)
			}
			if it.Descripcion != nil && err == nil {
				items = append(items, domain.Item{Descripcion: *it.Descripcion, Precio: precio})
			}
		}
	}

	if len(errores) > 0 {
		return "", nil, errores
	}
	return email, items, nil
}

// leerPrecio exige un número JSON (un string como "100" es inválido) mayor o igual a 0.
func leerPrecio(crudo json.RawMessage) (decimal.Decimal, error) {
	crudo = bytes.TrimSpace(crudo)
	if len(crudo) == 0 || string(crudo) == "null" {
		return decimal.Decimal{}, errors.New("es obligatorio")
	}
	if crudo[0] == '"' {
		return decimal.Decimal{}, errors.New("debe ser un número, no un texto")
	}
	precio, err := decimal.NewFromString(string(crudo))
	if err != nil {
		return decimal.Decimal{}, errors.New("debe ser un número")
	}
	if precio.IsNegative() {
		return decimal.Decimal{}, errors.New("debe ser mayor o igual a 0")
	}
	return precio, nil
}

// emailValido acepta solo una dirección simple (ana@example.com), no la forma
// con nombre ("Ana <ana@example.com>").
func emailValido(e string) bool {
	direccion, err := mail.ParseAddress(e)
	return err == nil && direccion.Address == e && strings.Contains(e, "@")
}

func errorDeDecodificacion(err error) errorCampo {
	var tipo *json.UnmarshalTypeError
	var sintaxis *json.SyntaxError
	switch {
	case errors.As(err, &tipo):
		return errorCampo{Campo: tipo.Field, Mensaje: "tipo de dato inválido"}
	case errors.As(err, &sintaxis), errors.Is(err, io.ErrUnexpectedEOF):
		return errorCampo{Campo: "", Mensaje: "el cuerpo no es un JSON válido"}
	case errors.Is(err, io.EOF):
		return errorCampo{Campo: "", Mensaje: "falta el cuerpo de la solicitud"}
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		campo := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return errorCampo{Campo: campo, Mensaje: "campo no definido en el contrato"}
	case strings.Contains(err.Error(), "request body too large"):
		return errorCampo{Campo: "", Mensaje: "el cuerpo supera el tamaño máximo de 1 MiB"}
	default:
		return errorCampo{Campo: "", Mensaje: "cuerpo inválido"}
	}
}

// importe se serializa como número JSON con 2 decimales (31500.00).
type importe decimal.Decimal

func (i importe) MarshalJSON() ([]byte, error) {
	return []byte(decimal.Decimal(i).StringFixed(2)), nil
}

type itemSalida struct {
	Descripcion string  `json:"descripcion"`
	Precio      importe `json:"precio"`
}

// comprobanteSalida es el esquema Comprobante del contrato.
type comprobanteSalida struct {
	ID           string       `json:"id"`
	Numero       string       `json:"numero"`
	Email        string       `json:"email"`
	Items        []itemSalida `json:"items"`
	Total        importe      `json:"total"`
	FechaEmision string       `json:"fechaEmision"`
	EstadoEnvio  string       `json:"estadoEnvio"`
}

func aSalida(c *domain.Comprobante) comprobanteSalida {
	items := make([]itemSalida, len(c.Items))
	for i, it := range c.Items {
		items[i] = itemSalida{Descripcion: it.Descripcion, Precio: importe(it.Precio)}
	}
	return comprobanteSalida{
		ID:           c.ID,
		Numero:       c.Numero(),
		Email:        c.Email,
		Items:        items,
		Total:        importe(c.Total),
		FechaEmision: c.FechaEmision.UTC().Format(time.RFC3339),
		EstadoEnvio:  string(c.EstadoEnvio),
	}
}
