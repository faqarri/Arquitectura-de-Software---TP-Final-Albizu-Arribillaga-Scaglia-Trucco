package catalogo_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestReglaDeCapas verifica la arquitectura en capas de ADR-002:
// handlers → services → repository, y models como base común. Una capa solo
// importa la de abajo: handlers no accede a la base y repository no conoce HTTP.
func TestReglaDeCapas(t *testing.T) {
	const raiz = "agencia/catalogo/internal/"
	reglas := []struct {
		paquete    string
		prohibidos []string
	}{
		{"./internal/handlers", []string{
			raiz + "repository", raiz + "events", raiz + "jobs",
			"database/sql", "go.mongodb.org/", "github.com/redis/", "github.com/go-sql-driver/",
		}},
		{"./internal/services", []string{
			raiz + "handlers", raiz + "events", raiz + "jobs",
			"github.com/gin-gonic/", "net/http", "github.com/rabbitmq/",
		}},
		{"./internal/repository", []string{
			raiz + "handlers", raiz + "services", raiz + "events", raiz + "jobs",
			"github.com/gin-gonic/", "net/http",
		}},
		{"./internal/events", []string{raiz + "repository", raiz + "handlers", "database/sql"}},
		{"./internal/jobs", []string{raiz + "repository", raiz + "handlers", "database/sql"}},
	}
	for _, r := range reglas {
		salida, err := exec.Command("go", "list", "-f", `{{join .Imports " "}}`, r.paquete).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", r.paquete, err)
		}
		for _, imp := range strings.Fields(string(salida)) {
			for _, p := range r.prohibidos {
				if strings.HasPrefix(imp, p) {
					t.Errorf("%s importa %s, que su capa no puede usar", r.paquete, imp)
				}
			}
		}
	}

	// models es la base: no depende de ninguna capa ni de infraestructura.
	salida, err := exec.Command("go", "list", "-deps", "./internal/models").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, dep := range strings.Fields(string(salida)) {
		for _, p := range []string{raiz + "handlers", raiz + "services", raiz + "repository",
			"github.com/gin-gonic/", "database/sql", "go.mongodb.org/", "github.com/redis/", "github.com/rabbitmq/"} {
			if strings.HasPrefix(dep, p) {
				t.Errorf("internal/models depende de %s", dep)
			}
		}
	}
}
