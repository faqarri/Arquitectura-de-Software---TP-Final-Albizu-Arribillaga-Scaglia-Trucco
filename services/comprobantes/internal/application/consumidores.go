package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
)

// ErrClaveInvalida: la API Key no existe.
var ErrClaveInvalida = errors.New("API Key inválida")

// prefijoClave identifica las claves de esta API (por ejemplo, en un escáner
// de secretos o en un .env lleno de variables).
const prefijoClave = "cmp_"

// HashClave es el hash con el que se guarda una API Key. La clave tiene 256
// bits aleatorios, así que un SHA-256 sin sal alcanza: no se puede adivinar
// por fuerza bruta como una contraseña elegida por una persona.
func HashClave(clave string) string {
	suma := sha256.Sum256([]byte(clave))
	return hex.EncodeToString(suma[:])
}

func generarClave() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefijoClave + base64.RawURLEncoding.EncodeToString(b), nil
}

// SolicitudRegistro es lo que manda un grupo para obtener su API Key.
type SolicitudRegistro struct {
	Grupo string
	Email string
}

// RegistrarConsumidor genera una API Key para un grupo y se la envía por correo
// al email indicado. La clave no se devuelve en la respuesta: que llegue al
// correo demuestra que el email es del grupo.
type RegistrarConsumidor struct {
	repo       ports.RepositorioConsumidores
	mailer     ports.Mailer
	reservados map[string]bool
	generar    func() (string, error)
}

// NuevoRegistrarConsumidor recibe los nombres de grupo que no se pueden
// registrar: el consumidor interno y los grupos con clave fija por configuración.
func NuevoRegistrarConsumidor(repo ports.RepositorioConsumidores, mailer ports.Mailer, reservados []string) *RegistrarConsumidor {
	r := map[string]bool{domain.ConsumidorInterno: true}
	for _, g := range reservados {
		r[g] = true
	}
	return &RegistrarConsumidor{repo: repo, mailer: mailer, reservados: r, generar: generarClave}
}

// Ejecutar valida el grupo, envía la clave por correo y la registra. Si el
// grupo ya existe con el mismo email, le agrega una clave nueva (sirve para
// recuperar una clave perdida) y las anteriores siguen vigentes.
func (uc *RegistrarConsumidor) Ejecutar(ctx context.Context, s SolicitudRegistro) error {
	if err := domain.ValidarGrupo(s.Grupo); err != nil {
		return err
	}
	if uc.reservados[s.Grupo] {
		return domain.ErrGrupoReservado
	}
	email, err := uc.repo.EmailDelGrupo(ctx, s.Grupo)
	switch {
	case errors.Is(err, ports.ErrNoEncontrado):
	case err != nil:
		return err
	case !strings.EqualFold(email, s.Email):
		return ports.ErrGrupoRegistrado
	}

	clave, err := uc.generar()
	if err != nil {
		return fmt.Errorf("generar API Key: %w", err)
	}
	// Primero el correo y después el registro: si el correo falla no queda una
	// clave guardada que nadie recibió.
	if err := uc.mailer.Enviar(ctx, correoDeClave(s.Grupo, s.Email, clave)); err != nil {
		return fmt.Errorf("enviar API Key: %w", err)
	}
	// Si dos registros del mismo grupo nuevo compiten, AgregarClave devuelve
	// ErrGrupoRegistrado al que llegó segundo con otro email.
	return uc.repo.AgregarClave(ctx, s.Grupo, s.Email, HashClave(clave))
}

func correoDeClave(grupo, email, clave string) ports.Correo {
	return ports.Correo{
		Para:   email,
		Asunto: "Tu API Key de la API de Comprobantes",
		Cuerpo: fmt.Sprintf(`Hola, grupo %s:

Esta es tu API Key para la API de Emisión de Comprobantes:

    %s

Cómo usarla:
  - Guardala en la configuración de tu proyecto (por ejemplo, en tu .env como
    COMPROBANTES_API_KEY), nunca en el repositorio.
  - Mandala en el header X-API-Key de cada solicitud a /v1/comprobantes.

No la vamos a volver a mostrar. Si la perdés, pedí otra con el mismo grupo y
el mismo email: te llega una nueva y la anterior sigue funcionando.

Los pasos completos están en la guía de integración que te compartimos.
`, grupo, clave),
	}
}

// AutenticarConsumidor resuelve qué grupo es dueño de una API Key: primero las
// claves fijas de la configuración y después las registradas.
type AutenticarConsumidor struct {
	repo  ports.RepositorioConsumidores
	fijas map[string]string
}

// NuevoAutenticarConsumidor recibe las claves fijas (clave → grupo). repo puede
// ser nil si solo se usan claves fijas (por ejemplo, en tests).
func NuevoAutenticarConsumidor(repo ports.RepositorioConsumidores, fijas map[string]string) *AutenticarConsumidor {
	return &AutenticarConsumidor{repo: repo, fijas: fijas}
}

// Ejecutar devuelve el grupo dueño de la clave, o ErrClaveInvalida.
func (uc *AutenticarConsumidor) Ejecutar(ctx context.Context, clave string) (string, error) {
	if clave == "" {
		return "", ErrClaveInvalida
	}
	if grupo, ok := uc.fijas[clave]; ok {
		return grupo, nil
	}
	if uc.repo == nil {
		return "", ErrClaveInvalida
	}
	grupo, err := uc.repo.GrupoPorClave(ctx, HashClave(clave))
	if errors.Is(err, ports.ErrNoEncontrado) {
		return "", ErrClaveInvalida
	}
	return grupo, err
}
