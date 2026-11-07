package mysql

// Tests de integración contra MySQL real (contenedor mysql:8.4, la misma imagen
// del docker-compose). Necesitan Docker; con go test -short se saltean.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"gorm.io/gorm"

	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

var db *gorm.DB

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	c, err := tcmysql.Run(ctx, "mysql:8.4",
		tcmysql.WithDatabase("reservas"), tcmysql.WithUsername("reservas"), tcmysql.WithPassword("prueba"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "no se pudo levantar MySQL (¿Docker está corriendo?):", err)
		os.Exit(1)
	}
	codigo := func() int {
		host, _ := c.Host(ctx)
		puerto, _ := c.MappedPort(ctx, "3306/tcp")
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		db, err = Conectar(ctx, Config{Host: host, Puerto: puerto.Port(), Usuario: "reservas", Password: "prueba", Nombre: "reservas"}, log)
		if err == nil {
			err = Migrar(db)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return m.Run()
	}()
	_ = c.Terminate(ctx)
	os.Exit(codigo)
}

func requiereMySQL(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("test de integración: se saltea con -short")
	}
}

var ctx = context.Background()

func nuevoItem(usuario string, retenciones ...string) domain.ItemCarrito {
	return domain.ItemCarrito{
		ID: uuid.NewString(), UsuarioID: usuario, IdempotencyKey: uuid.NewString(), OfertaID: "of-1", Cantidad: 1,
		Descripcion: "Vuelo a Bariloche", Destino: "Bariloche", PrecioReferencia: decimal.RequireFromString("1500.50"),
		Retenciones: retenciones, ExpiraEn: time.Now().Add(15 * time.Minute).UTC().Truncate(time.Millisecond),
		Estado: domain.ItemActivo, CreadoEn: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestCarrito(t *testing.T) {
	requiereMySQL(t)
	repo := NuevoCarritos(db)
	usuario := uuid.NewString()
	r1, r2 := uuid.NewString(), uuid.NewString()
	it := nuevoItem(usuario, r1, r2)

	if err := repo.Agregar(ctx, it); err != nil {
		t.Fatal(err)
	}
	// Un reintento trae otro id y otras retenciones, con la misma clave de idempotencia.
	reintento := nuevoItem(usuario, uuid.NewString())
	reintento.IdempotencyKey = it.IdempotencyKey
	if err := repo.Agregar(ctx, reintento); err != ports.ErrClaveDuplicada {
		t.Errorf("la misma clave: %v; se esperaba ErrClaveDuplicada", err)
	}
	por, err := repo.BuscarPorClave(ctx, usuario, it.IdempotencyKey)
	if err != nil || por.ID != it.ID || len(por.Retenciones) != 2 || por.Retenciones[0] != r1 {
		t.Fatalf("por=%+v err=%v", por, err)
	}
	if !por.PrecioReferencia.Equal(it.PrecioReferencia) || !por.ExpiraEn.Equal(it.ExpiraEn) {
		t.Errorf("precio o vencimiento cambiaron: %+v", por)
	}

	if err := repo.MarcarVencida(ctx, r2); err != nil {
		t.Fatal(err)
	}
	c, _ := repo.Obtener(ctx, usuario)
	if c.Items[0].Estado != domain.ItemVencido {
		t.Errorf("estado = %s", c.Items[0].Estado)
	}
	if err := repo.MarcarVencida(ctx, "desconocida"); err != nil {
		t.Errorf("una retención desconocida no es un error: %v", err)
	}

	if err := repo.Quitar(ctx, "otro-usuario", it.ID); err != nil {
		t.Fatal(err)
	}
	if c, _ := repo.Obtener(ctx, usuario); len(c.Items) != 1 {
		t.Error("otro usuario no puede quitar el ítem")
	}
	if err := repo.Quitar(ctx, usuario, it.ID); err != nil {
		t.Fatal(err)
	}
	if c, _ := repo.Obtener(ctx, usuario); !c.Vacio() {
		t.Error("el carrito debía quedar vacío")
	}
}

func compraDeCarrito(t *testing.T, usuario string, items ...domain.ItemCarrito) *domain.Compra {
	t.Helper()
	c, err := domain.NuevaCompra(usuario, "ana@example.com", uuid.NewString(), domain.Carrito{UsuarioID: usuario, Items: items}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c.CreadaEn = c.CreadaEn.Truncate(time.Millisecond)
	c.ActualizadaEn = c.CreadaEn
	if err := NuevoCompras(db).Crear(ctx, c); err != nil {
		t.Fatal(err)
	}
	return c
}

func contarOutbox(t *testing.T, compraID string) int {
	t.Helper()
	var filas []outboxModelo
	if err := db.Where("tipo = ?", TipoCompraConfirmada).Find(&filas).Error; err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range filas {
		var ev eventoCompraConfirmada
		_ = json.Unmarshal([]byte(f.Payload), &ev)
		if ev.CompraID == compraID {
			n++
		}
	}
	return n
}

func TestConfirmarUnaCompraPublicaUnaSolaVezAunqueSeResuelvaEnParalelo(t *testing.T) {
	requiereMySQL(t)
	usuario := uuid.NewString()
	it := nuevoItem(usuario, uuid.NewString())
	_ = NuevoCarritos(db).Agregar(ctx, it)
	c := compraDeCarrito(t, usuario, it)
	reintento := *c
	reintento.ID = uuid.NewString()
	if err := NuevoCompras(db).Crear(ctx, &reintento); err != ports.ErrClaveDuplicada {
		t.Errorf("la misma clave: %v", err)
	}

	lineas := []domain.LineaCompra{{RetencionID: it.Retenciones[0], OfertaID: "of-1", Descripcion: "Vuelo a Bariloche x1", Destino: "Bariloche", Precio: decimal.RequireFromString("1500.50")}}
	var wg sync.WaitGroup
	resueltas := make(chan bool, 5)
	for range 5 {
		wg.Go(func() {
			copia := *c
			_ = copia.Confirmar(lineas, time.Now())
			ok, err := NuevoCompras(db).Resolver(ctx, &copia)
			if err != nil {
				t.Error(err)
			}
			resueltas <- ok
		})
	}
	wg.Wait()
	close(resueltas)
	n := 0
	for ok := range resueltas {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Errorf("la resolvieron %d procesos; tenía que ser exactamente 1", n)
	}
	if got := contarOutbox(t, c.ID); got != 1 {
		t.Errorf("CompraConfirmada en el outbox: %d; se esperaba 1", got)
	}

	final, err := NuevoCompras(db).Obtener(ctx, usuario, c.ID)
	if err != nil || final.Estado != domain.CompraConfirmada || !final.Total.Equal(decimal.RequireFromString("1500.50")) || len(final.Lineas) != 1 {
		t.Fatalf("final=%+v err=%v", final, err)
	}
	if carrito, _ := NuevoCarritos(db).Obtener(ctx, usuario); !carrito.Vacio() {
		t.Error("los ítems comprados debían salir del carrito")
	}

	// El evento tiene el formato que espera Comprobantes.
	var f outboxModelo
	db.Where("tipo = ?", TipoCompraConfirmada).Order("id DESC").Take(&f)
	var ev map[string]any
	_ = json.Unmarshal([]byte(f.Payload), &ev)
	items := ev["items"].([]any)
	if ev["email"] != "ana@example.com" || items[0].(map[string]any)["precio"] != 1500.5 || ev["eventId"] == "" {
		t.Errorf("evento = %s", f.Payload)
	}
}

func TestRechazarMarcaVencidosYNoPublica(t *testing.T) {
	requiereMySQL(t)
	usuario := uuid.NewString()
	vencido, vigente := nuevoItem(usuario, uuid.NewString()), nuevoItem(usuario, uuid.NewString())
	_ = NuevoCarritos(db).Agregar(ctx, vencido)
	_ = NuevoCarritos(db).Agregar(ctx, vigente)
	c := compraDeCarrito(t, usuario, vencido, vigente)

	_ = c.Rechazar(vencido.Retenciones, time.Now())
	if ok, err := NuevoCompras(db).Resolver(ctx, c); !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if contarOutbox(t, c.ID) != 0 {
		t.Error("una compra rechazada no publica")
	}
	carrito, _ := NuevoCarritos(db).Obtener(ctx, usuario)
	estados := map[string]domain.EstadoItem{}
	for _, it := range carrito.Items {
		estados[it.ID] = it.Estado
	}
	if estados[vencido.ID] != domain.ItemVencido || estados[vigente.ID] != domain.ItemActivo {
		t.Errorf("estados = %v", estados)
	}
	final, _ := NuevoCompras(db).Obtener(ctx, usuario, c.ID)
	if final.Estado != domain.CompraRechazada || final.RetencionesVencidas[0] != vencido.Retenciones[0] {
		t.Errorf("final = %+v", final)
	}
}

func TestPendientesParaRecuperar(t *testing.T) {
	requiereMySQL(t)
	usuario := uuid.NewString()
	c := compraDeCarrito(t, usuario, nuevoItem(usuario, uuid.NewString()))
	pend, err := NuevoCompras(db).Pendientes(ctx, time.Now().Add(time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	encontrada := false
	for _, p := range pend {
		encontrada = encontrada || p.ID == c.ID
	}
	if !encontrada {
		t.Error("la compra pendiente no apareció")
	}
}

func TestOutbox(t *testing.T) {
	requiereMySQL(t)
	o := NuevoOutbox(db)
	if err := db.Create(&outboxModelo{Tipo: "X", Payload: `{}`, Headers: `{"X-Correlation-Id":"c1"}`}).Error; err != nil {
		t.Fatal(err)
	}
	pend, err := o.Pendientes(ctx, 1000)
	if err != nil || len(pend) == 0 {
		t.Fatalf("pend=%v err=%v", pend, err)
	}
	ultimo := pend[len(pend)-1]
	if ultimo.Headers["X-Correlation-Id"] != "c1" {
		t.Errorf("headers = %v", ultimo.Headers)
	}
	if err := o.MarcarPublicado(ctx, ultimo.ID); err != nil {
		t.Fatal(err)
	}
	pend, _ = o.Pendientes(ctx, 1000)
	for _, p := range pend {
		if p.ID == ultimo.ID {
			t.Error("el evento marcado sigue pendiente")
		}
	}
}

func TestGuias(t *testing.T) {
	requiereMySQL(t)
	repo := NuevoGuias(db)
	g, err := domain.NuevaGuia(uuid.NewString(), domain.SolicitudGuia{Titulo: "Trekking", Destino: "Bariloche", Autor: "Ana", Etiquetas: []string{"trekking"}, PDF: []byte("%PDF-1.4")}, []string{"Bariloche"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Crear(ctx, g, []byte("%PDF-1.4")); err != nil {
		t.Fatal(err)
	}
	porEnviar, _ := repo.PorEnviar(ctx, 100)
	encontrada := false
	for _, p := range porEnviar {
		if p.Guia.ID == g.ID {
			encontrada = string(p.PDF) == "%PDF-1.4" && p.Guia.Etiquetas[0] == "trekking"
		}
	}
	if !encontrada {
		t.Fatal("la guía por enviar no apareció con su PDF")
	}

	g.Registrada("doc12345", time.Now())
	if err := repo.Actualizar(ctx, g); err != nil {
		t.Fatal(err)
	}
	var archivos int64
	db.Model(&archivoGuiaModelo{}).Where("guia_id = ?", g.ID).Count(&archivos)
	if archivos != 0 {
		t.Error("el PDF debía borrarse una vez aceptada la guía")
	}
	listadas, _ := repo.Listar(ctx, g.UsuarioID)
	if len(listadas) != 1 || listadas[0].Estado != domain.GuiaPendiente || listadas[0].DocumentoID != "doc12345" {
		t.Errorf("listadas = %+v", listadas)
	}
}
