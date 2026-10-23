package comprobantes_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNucleoNoDependeDeInfraestructura verifica la regla de dependencias de la
// arquitectura hexagonal (ADR-002): el dominio, los puertos y los casos de uso no
// pueden importar frameworks, drivers ni adaptadores.
func TestNucleoNoDependeDeInfraestructura(t *testing.T) {
	prohibidos := []string{
		"github.com/gin-gonic/",
		"gorm.io/",
		"github.com/rabbitmq/",
		"github.com/go-sql-driver/",
		"net/smtp",
		"agencia/comprobantes/internal/adapters",
	}
	for _, paquete := range []string{"./internal/domain", "./internal/ports", "./internal/application"} {
		salida, err := exec.Command("go", "list", "-deps", paquete).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", paquete, err)
		}
		for _, dep := range strings.Fields(string(salida)) {
			for _, p := range prohibidos {
				if strings.HasPrefix(dep, p) {
					t.Errorf("%s depende de %s", paquete, dep)
				}
			}
		}
	}
}
