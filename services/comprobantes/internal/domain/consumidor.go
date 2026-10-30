package domain

import (
	"errors"
	"regexp"
)

// Un consumidor es un grupo externo que usa la API pública. Se identifica por
// su nombre de grupo, que separa sus comprobantes y sus claves de idempotencia
// de los de los demás grupos.

var (
	ErrGrupoInvalido  = errors.New("el grupo debe tener entre 3 y 50 caracteres: letras minúsculas, números y guiones, empezando por letra o número")
	ErrGrupoReservado = errors.New("ese nombre de grupo está reservado")
)

var formatoGrupo = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,49}$`)

// ValidarGrupo verifica el formato del nombre de grupo. No sabe qué nombres
// están reservados: eso lo decide el caso de uso.
func ValidarGrupo(grupo string) error {
	if !formatoGrupo.MatchString(grupo) {
		return ErrGrupoInvalido
	}
	return nil
}
