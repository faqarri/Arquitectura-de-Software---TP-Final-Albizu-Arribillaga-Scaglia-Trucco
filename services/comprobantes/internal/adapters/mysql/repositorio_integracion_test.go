package mysql_test

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"agencia/comprobantes/internal/adapters/mysql"
	"agencia/comprobantes/internal/application"
	"agencia/comprobantes/internal/domain"
	"agencia/comprobantes/internal/ports"
	"agencia/pkg/observabilidad"
)

// Tests de integración contra un MySQL real (requisito 2.11). Levantan un
// contenedor mysql:8.4, la misma imagen del docker-compose. Necesitan Docker;
// con `go test -short` se saltean.

var repo *mysql.Repositorio

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	contenedor, err := tcmysql.Run(ctx, "mysql:8.4",
		tcmysql.WithDatabase("comprobantes"),
		tcmysql.WithUsername("comprobantes"),
		tcmysql.WithPassword("prueba"),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no se pudo levantar MySQL (¿Docker está corriendo?):", err)
		os.Exit(1)
	}
	codigo := ejecutar(ctx, contenedor, m)
	_ = contenedor.Terminate(ctx)
	os.Exit(codigo)
}

func ejecutar(ctx context.Context, contenedor *tcmysql.MySQLContainer, m *testing.M) int {
	host, err := contenedor.Host(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	puerto, err := contenedor.MappedPort(ctx, "3306/tcp")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	db, err := mysql.Conectar(mysql.Config{
		Host: host, Puerto: puerto.Port(), Usuario: "comprobantes", Password: "prueba", Nombre: "comprobantes",
	})
	if err == nil {
		err = mysql.Migrar(db)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	repo = mysql.NuevoRepositorio(db)
	return m.Run()
}

func requiereMySQL(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("test de integración: se saltea con -short")
	}
}

func nuevoComprobante(t *testing.T, consumidor, clave string, precios ...string) *domain.Comprobante {
	t.Helper()
	items := make([]domain.Item, len(precios))
	for i, p := range precios {
		items[i] = domain.Item{Descripcion: fmt.Sprintf("Ítem %d", i+1), Precio: decimal.RequireFromString(p)}
	}
	c, err := domain.NuevoComprobante(consumidor, clave, "hash-"+clave, "ana@example.com", items, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGuardarYBuscar(t *testing.T) {
	requiereMySQL(t)
	ctx := context.Background()
	c := nuevoComprobante(t, "grupo-a", uuid.NewString(), "30000", "1500.50", "0.10")

	if err := repo.Guardar(ctx, c); err != nil {
		t.Fatal(err)
	}
	if c.Nro <= 0 {
		t.Fatalf("Guardar no completó el número del comprobante (Nro = %d)", c.Nro)
	}

	leido, err := repo.BuscarPorID(ctx, "grupo-a", c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if leido.Nro != c.Nro || leido.Email != c.Email || leido.EstadoEnvio != domain.EnvioPendiente {
		t.Errorf("comprobante leído distinto del guardado: %+v", leido)
	}
	if !leido.Total.Equal(decimal.RequireFromString("31500.60")) {
		t.Errorf("total %s, se esperaba 31500.60", leido.Total)
	}
	if len(leido.Items) != 3 || leido.Items[1].Descripcion != "Ítem 2" || !leido.Items[1].Precio.Equal(decimal.RequireFromString("1500.50")) {
		t.Errorf("ítems leídos fuera de orden o con otro precio: %+v", leido.Items)
	}
	if !leido.FechaEmision.Equal(c.FechaEmision) {
		t.Errorf("fecha %v, se esperaba %v", leido.FechaEmision, c.FechaEmision)
	}

	porClave, err := repo.BuscarPorClave(ctx, "grupo-a", c.IdempotencyKey)
	if err != nil || porClave.ID != c.ID {
		t.Errorf("BuscarPorClave: %v, %v", porClave, err)
	}
}

func TestNumerosCorrelativos(t *testing.T) {
	requiereMySQL(t)
	ctx := context.Background()
	a := nuevoComprobante(t, "grupo-a", uuid.NewString(), "10")
	b := nuevoComprobante(t, "grupo-a", uuid.NewString(), "10")

	if err := repo.Guardar(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := repo.Guardar(ctx, b); err != nil {
		t.Fatal(err)
	}
	if b.Nro <= a.Nro {
		t.Errorf("números no correlativos: %d y después %d", a.Nro, b.Nro)
	}
}

func TestOtroConsumidorNoVeElComprobante(t *testing.T) {
	requiereMySQL(t)
	ctx := context.Background()
	c := nuevoComprobante(t, "grupo-a", uuid.NewString(), "100")
	if err := repo.Guardar(ctx, c); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.BuscarPorID(ctx, "grupo-b", c.ID); !errors.Is(err, ports.ErrNoEncontrado) {
		t.Errorf("otro consumidor: error %v, se esperaba ErrNoEncontrado", err)
	}
	if _, err := repo.Obtener(ctx, c.ID); err != nil {
		t.Errorf("Obtener (sin filtro de consumidor): %v", err)
	}
	if _, err := repo.Obtener(ctx, uuid.NewString()); !errors.Is(err, ports.ErrNoEncontrado) {
		t.Errorf("id inexistente: error %v, se esperaba ErrNoEncontrado", err)
	}
}

func TestClaveDuplicada(t *testing.T) {
	requiereMySQL(t)
	ctx := context.Background()
	clave := uuid.NewString()

	if err := repo.Guardar(ctx, nuevoComprobante(t, "grupo-a", clave, "100")); err != nil {
		t.Fatal(err)
	}
	err := repo.Guardar(ctx, nuevoComprobante(t, "grupo-a", clave, "100"))
	if !errors.Is(err, ports.ErrClaveDuplicada) {
		t.Errorf("misma clave y consumidor: error %v, se esperaba ErrClaveDuplicada", err)
	}
	if err := repo.Guardar(ctx, nuevoComprobante(t, "grupo-b", clave, "100")); err != nil {
		t.Errorf("misma clave en otro consumidor debería guardarse: %v", err)
	}
}

func TestGuardarDejaElEventoEnElOutbox(t *testing.T) {
	requiereMySQL(t)
	ctx := observabilidad.ConCorrelacion(context.Background(), "correlacion-de-prueba")
	c := nuevoComprobante(t, "grupo-a", uuid.NewString(), "100")
	if err := repo.Guardar(ctx, c); err != nil {
		t.Fatal(err)
	}

	evento := buscarEventoDe(t, c.ID)
	if evento == nil {
		t.Fatal("no hay evento pendiente en el outbox para el comprobante")
	}
	if evento.Tipo != ports.EventoEnvioSolicitado {
		t.Errorf("tipo %q, se esperaba %q", evento.Tipo, ports.EventoEnvioSolicitado)
	}
	if evento.Headers[observabilidad.HeaderCorrelacion] != "correlacion-de-prueba" {
		t.Errorf("headers %v sin la correlación de la solicitud", evento.Headers)
	}

	if err := repo.MarcarPublicado(ctx, evento.ID); err != nil {
		t.Fatal(err)
	}
	if buscarEventoDe(t, c.ID) != nil {
		t.Error("el evento sigue pendiente después de MarcarPublicado")
	}
}

func buscarEventoDe(t *testing.T, comprobanteID string) *ports.EventoOutbox {
	t.Helper()
	pendientes, err := repo.Pendientes(context.Background(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range pendientes {
		var payload struct {
			ComprobanteID string `json:"comprobanteId"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.ComprobanteID == comprobanteID {
			return &e
		}
	}
	return nil
}

func TestActualizarEnvio(t *testing.T) {
	requiereMySQL(t)
	ctx := context.Background()
	c := nuevoComprobante(t, "grupo-a", uuid.NewString(), "100")
	if err := repo.Guardar(ctx, c); err != nil {
		t.Fatal(err)
	}

	if err := c.MarcarEnviado(); err != nil {
		t.Fatal(err)
	}
	if err := repo.ActualizarEnvio(ctx, c); err != nil {
		t.Fatal(err)
	}
	leido, err := repo.Obtener(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if leido.EstadoEnvio != domain.EnvioEnviado || leido.IntentosEnvio != 1 {
		t.Errorf("estado %s con %d intentos, se esperaba ENVIADO con 1", leido.EstadoEnvio, leido.IntentosEnvio)
	}
}

// TestEmitirConRepositorioReal recorre el caso de uso completo contra la base:
// un reintento con el mismo cuerpo devuelve el comprobante ya emitido y uno con
// otro cuerpo es un conflicto.
func TestEmitirConRepositorioReal(t *testing.T) {
	requiereMySQL(t)
	ctx := context.Background()
	emitir := application.NuevoEmitirComprobante(repo)
	solicitud := func(precio string) application.SolicitudEmision {
		return application.SolicitudEmision{
			Consumidor:     "grupo-a",
			IdempotencyKey: "orden-42",
			Email:          "ana@example.com",
			Items:          []domain.Item{{Descripcion: "Entrada", Precio: decimal.RequireFromString(precio)}},
		}
	}

	primero, err := emitir.Ejecutar(ctx, solicitud("100"))
	if err != nil || primero.YaExistia {
		t.Fatalf("primera emisión: %+v, %v", primero, err)
	}
	reintento, err := emitir.Ejecutar(ctx, solicitud("100"))
	if err != nil || !reintento.YaExistia || reintento.Comprobante.ID != primero.Comprobante.ID {
		t.Errorf("reintento: %+v, %v", reintento, err)
	}
	if _, err := emitir.Ejecutar(ctx, solicitud("200")); !errors.Is(err, application.ErrConflictoIdempotencia) {
		t.Errorf("otro cuerpo con la misma clave: error %v, se esperaba ErrConflictoIdempotencia", err)
	}
}
