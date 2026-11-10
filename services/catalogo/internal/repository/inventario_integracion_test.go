package repository

// Tests de integración contra MySQL real (contenedor mysql:8.4, la misma imagen
// del docker-compose). Verifican la operación crítica de ADR-014: retener,
// liberar, confirmar y vencer con bloqueos, idempotencia y concurrencia.
// Necesitan Docker; con go test -short se saltean.

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"agencia/catalogo/internal/models"
)

var (
	db      *sql.DB
	inv     *Inventario
	mongoDB *mongo.Database
	ctx     = context.Background()
)

const (
	minutos      = 15
	maxPorOferta = 6
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	c, err := tcmysql.Run(ctx, "mysql:8.4",
		tcmysql.WithDatabase("catalogo"), tcmysql.WithUsername("catalogo"), tcmysql.WithPassword("prueba"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "no se pudo levantar MySQL (¿Docker está corriendo?):", err)
		os.Exit(1)
	}
	cm, err := tcmongo.Run(ctx, "mongo:7")
	if err != nil {
		_ = c.Terminate(ctx)
		fmt.Fprintln(os.Stderr, "no se pudo levantar MongoDB:", err)
		os.Exit(1)
	}
	codigo := func() int {
		host, _ := c.Host(ctx)
		puerto, _ := c.MappedPort(ctx, "3306/tcp")
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		db, err = ConectarMySQL(ctx, ConfigMySQL{Host: host, Puerto: puerto.Port(), Usuario: "catalogo", Password: "prueba", Nombre: "catalogo"}, log)
		if err == nil {
			err = MigrarMySQL(ctx, db)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		inv = NuevoInventario(db)

		uri, err := cm.ConnectionString(ctx)
		if err == nil {
			mongoDB, err = ConectarMongo(ctx, uri, "catalogo", log)
		}
		if err == nil {
			err = MigrarMongo(ctx, mongoDB)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return m.Run()
	}()
	_ = c.Terminate(ctx)
	_ = cm.Terminate(ctx)
	os.Exit(codigo)
}

func requiereMySQL(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("test de integración: se saltea con -short")
	}
}

func nuevaOferta(t *testing.T, cupo int) string {
	t.Helper()
	id := "of-" + uuid.NewString()
	if err := inv.CrearInventario(ctx, id, cupo); err != nil {
		t.Fatal(err)
	}
	return id
}

func solicitud(titular string, cantidad int, ofertas ...string) SolicitudRetencion {
	s := SolicitudRetencion{IdempotencyKey: uuid.NewString(), TitularID: titular, Minutos: minutos, MaxPorOferta: maxPorOferta}
	for _, o := range ofertas {
		s.Items = append(s.Items, ItemRetencion{OfertaID: o, Cantidad: cantidad, Descripcion: "Oferta " + o, Destino: "Bariloche", PrecioReferencia: 1000})
	}
	if len(ofertas) > 1 {
		s.PackID = "pk-prueba"
	}
	return s
}

func retenerOFallar(t *testing.T, s SolicitudRetencion) models.GrupoRetenido {
	t.Helper()
	g, err := inv.Retener(ctx, s)
	if err != nil {
		t.Fatalf("retener: %v", err)
	}
	return g
}

func lineasPara(g models.GrupoRetenido) map[string]models.LineaConfirmada {
	l := map[string]models.LineaConfirmada{}
	for _, r := range g.Retenciones {
		l[r.ID] = models.LineaConfirmada{Descripcion: r.Descripcion, Destino: r.Destino, Precio: r.PrecioReferencia}
	}
	return l
}

func idsDe(g models.GrupoRetenido) []string {
	var ids []string
	for _, r := range g.Retenciones {
		ids = append(ids, r.ID)
	}
	return ids
}

// vencer adelanta el vencimiento de las retenciones al pasado, como si hubieran pasado los 15 minutos.
func vencer(t *testing.T, g models.GrupoRetenido) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `UPDATE retenciones SET expira_en = NOW(6) - INTERVAL 1 SECOND WHERE grupo_id = ?`, g.GrupoID); err != nil {
		t.Fatal(err)
	}
}

func disponibilidad(t *testing.T, oferta string) models.Disponibilidad {
	t.Helper()
	d, err := inv.Disponibilidad(ctx, oferta)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func estadoRetencion(t *testing.T, id string) string {
	t.Helper()
	var e string
	if err := db.QueryRowContext(ctx, `SELECT estado FROM retenciones WHERE id = ?`, id).Scan(&e); err != nil {
		t.Fatal(err)
	}
	return e
}

// RN-01: 20 clientes intentan retener el último cupo al mismo tiempo. Exactamente uno gana.
func TestConcurrenciaUltimoCupoGanaUnoSolo(t *testing.T) {
	requiereMySQL(t)
	oferta := nuevaOferta(t, 1)

	const clientes = 20
	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		ganadores    int
		rechazos     = map[string]int{}
		erroresOtros []error
	)
	inicio := make(chan struct{})
	for i := range clientes {
		wg.Go(func() {
			<-inicio
			_, err := inv.Retener(ctx, solicitud(fmt.Sprintf("cliente-%d", i), 1, oferta))
			mu.Lock()
			defer mu.Unlock()
			var sinCupo *models.ErrSinCupo
			switch {
			case err == nil:
				ganadores++
			case errors.As(err, &sinCupo):
				rechazos[sinCupo.Codigo]++
			default:
				erroresOtros = append(erroresOtros, err)
			}
		})
	}
	close(inicio)
	wg.Wait()

	if len(erroresOtros) > 0 {
		t.Fatalf("errores inesperados: %v", erroresOtros)
	}
	if ganadores != 1 || rechazos[models.CodigoRetenida] != clientes-1 {
		t.Fatalf("esperaba 1 ganador y %d RETENIDA; obtuve %d ganadores y %v", clientes-1, ganadores, rechazos)
	}
	d := disponibilidad(t, oferta)
	if d.Estado != models.Retenida || d.Libres != 0 || d.ProximaLiberacion == nil {
		t.Fatalf("la oferta debería quedar RETENIDA: %+v", d)
	}
}

// RN-01: 40 clientes contra 10 cupos. Se retienen exactamente 10, ni uno más.
func TestConcurrenciaSinSobreventa(t *testing.T) {
	requiereMySQL(t)
	oferta := nuevaOferta(t, 10)
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() { _, _ = inv.Retener(ctx, solicitud(fmt.Sprintf("cliente-%d", i), 1, oferta)) })
	}
	wg.Wait()

	var retenidos int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(cantidad), 0) FROM retenciones WHERE oferta_id = ? AND estado = 'ACTIVA'`, oferta).
		Scan(&retenidos); err != nil {
		t.Fatal(err)
	}
	if retenidos != 10 {
		t.Fatalf("se retuvieron %d cupos de 10", retenidos)
	}
}

// Un reintento de Reservas con la misma Idempotency-Key, en serie o en paralelo,
// devuelve el mismo grupo y no crea retenciones nuevas.
func TestRetenerEsIdempotente(t *testing.T) {
	requiereMySQL(t)
	oferta := nuevaOferta(t, 50)
	s := solicitud("ana", 2, oferta)

	var wg sync.WaitGroup
	grupos := make([]models.GrupoRetenido, 8)
	errs := make([]error, 8)
	for i := range grupos {
		wg.Go(func() { grupos[i], errs[i] = inv.Retener(ctx, s) })
	}
	wg.Wait()
	for i := range grupos {
		if errs[i] != nil {
			t.Fatalf("reintento %d falló: %v", i, errs[i])
		}
		if grupos[i].GrupoID != grupos[0].GrupoID || grupos[i].Retenciones[0].ID != grupos[0].Retenciones[0].ID {
			t.Fatalf("los reintentos devolvieron grupos distintos: %+v vs %+v", grupos[i], grupos[0])
		}
	}
	if d := disponibilidad(t, oferta); d.Libres != 48 {
		t.Fatalf("quedan %d libres, esperaba 48 (una sola retención de 2)", d.Libres)
	}
}

// RN-12: un carrito no retiene más de 6 cupos de la misma oferta.
func TestLimitePorCarrito(t *testing.T) {
	requiereMySQL(t)
	oferta := nuevaOferta(t, 50)
	retenerOFallar(t, solicitud("beto", 5, oferta))

	var regla *models.ErrRegla
	if _, err := inv.Retener(ctx, solicitud("beto", 2, oferta)); !errors.As(err, &regla) || regla.Codigo != models.CodigoLimitePorCarrito {
		t.Fatalf("5 + 2 debería superar el límite: %v", err)
	}
	retenerOFallar(t, solicitud("beto", 1, oferta)) // 5 + 1 = 6, permitido
	retenerOFallar(t, solicitud("otro", 6, oferta)) // el límite es por titular
}

// RN-07: un pack retiene todas sus opciones o ninguna.
func TestPackTodoONada(t *testing.T) {
	requiereMySQL(t)
	vuelo, hotel := nuevaOferta(t, 5), nuevaOferta(t, 1)
	retenerOFallar(t, solicitud("otro", 1, hotel)) // el hotel queda sin lugar

	var sinCupo *models.ErrSinCupo
	if _, err := inv.Retener(ctx, solicitud("carla", 1, vuelo, hotel)); !errors.As(err, &sinCupo) || sinCupo.OfertaID != hotel {
		t.Fatalf("esperaba sin cupo en el hotel: %v", err)
	}
	if d := disponibilidad(t, vuelo); d.Libres != 5 {
		t.Fatalf("no debería haberse retenido el vuelo: quedan %d de 5", d.Libres)
	}

	g := retenerOFallar(t, solicitud("carla", 1, vuelo, nuevaOferta(t, 3)))
	if len(g.Retenciones) != 2 {
		t.Fatalf("el pack debería crear 2 retenciones: %+v", g)
	}
}

func TestOfertaInexistente(t *testing.T) {
	requiereMySQL(t)
	var inexistente *models.ErrOfertaInexistente
	if _, err := inv.Retener(ctx, solicitud("ana", 1, "of-no-existe")); !errors.As(err, &inexistente) {
		t.Fatalf("esperaba OFERTA_INEXISTENTE: %v", err)
	}
}

// RN-02: confirmar pasa los cupos a vendidos; un reintento devuelve lo mismo
// y no vuelve a sumar.
func TestConfirmarEsAtomicaEIdempotente(t *testing.T) {
	requiereMySQL(t)
	vuelo, hotel := nuevaOferta(t, 10), nuevaOferta(t, 10)
	g1 := retenerOFallar(t, solicitud("dani", 2, vuelo))
	g2 := retenerOFallar(t, solicitud("dani", 1, hotel))
	ids := append(idsDe(g1), idsDe(g2)...)
	lineas := lineasPara(g1)
	for k, v := range lineasPara(g2) {
		lineas[k] = v
	}
	compra := uuid.NewString()

	var wg sync.WaitGroup
	respuestas := make([]models.Confirmacion, 5)
	errs := make([]error, 5)
	for i := range respuestas {
		wg.Go(func() { respuestas[i], errs[i] = inv.Confirmar(ctx, compra, "dani", ids, lineas) })
	}
	wg.Wait()
	for i := range respuestas {
		if errs[i] != nil {
			t.Fatalf("confirmación %d falló: %v", i, errs[i])
		}
		if len(respuestas[i].Lineas) != 2 || respuestas[i].Total != 2000 {
			t.Fatalf("respuesta inesperada: %+v", respuestas[i])
		}
	}
	if _, vendidos, _ := inv.Cupo(ctx, vuelo); vendidos != 2 {
		t.Fatalf("vendidos del vuelo = %d, esperaba 2 (sin duplicar por los reintentos)", vendidos)
	}
	if d := disponibilidad(t, vuelo); d.Libres != 8 || d.Estado != models.Disponible {
		t.Fatalf("disponibilidad del vuelo: %+v", d)
	}
	if e := estadoRetencion(t, ids[0]); e != models.RetencionConfirmada {
		t.Fatalf("estado = %s", e)
	}
}

// RN-11 y RN-02: con una retención vencida no se confirma ninguna, y su cupo
// vuelve a estar libre aunque el barrido todavía no la haya marcado.
func TestConfirmarConRetencionVencidaNoConfirmaNada(t *testing.T) {
	requiereMySQL(t)
	vuelo, hotel := nuevaOferta(t, 1), nuevaOferta(t, 5)
	g1 := retenerOFallar(t, solicitud("eli", 1, vuelo))
	g2 := retenerOFallar(t, solicitud("eli", 1, hotel))
	vencer(t, g1)

	lineas := lineasPara(g1)
	for k, v := range lineasPara(g2) {
		lineas[k] = v
	}
	var noVigentes *models.ErrRetencionesNoVigentes
	_, err := inv.Confirmar(ctx, uuid.NewString(), "eli", append(idsDe(g1), idsDe(g2)...), lineas)
	if !errors.As(err, &noVigentes) || len(noVigentes.IDs) != 1 || noVigentes.IDs[0] != g1.Retenciones[0].ID {
		t.Fatalf("esperaba RETENCION_NO_VIGENTE solo para la vencida: %v", err)
	}
	if e := estadoRetencion(t, g2.Retenciones[0].ID); e != models.RetencionActiva {
		t.Fatalf("la retención vigente no debería cambiar: %s", e)
	}
	if _, vendidos, _ := inv.Cupo(ctx, hotel); vendidos != 0 {
		t.Fatal("no debería haberse vendido nada")
	}
	// El cupo del vuelo vuelve a estar libre para otro cliente.
	retenerOFallar(t, solicitud("otro", 1, vuelo))
}

func TestConfirmarRetencionDeOtroTitular(t *testing.T) {
	requiereMySQL(t)
	g := retenerOFallar(t, solicitud("fede", 1, nuevaOferta(t, 5)))
	var noVigentes *models.ErrRetencionesNoVigentes
	if _, err := inv.Confirmar(ctx, uuid.NewString(), "intruso", idsDe(g), lineasPara(g)); !errors.As(err, &noVigentes) {
		t.Fatalf("no se puede confirmar la retención de otro: %v", err)
	}
}

// Un pack se compra entero: confirmar solo una parte no confirma nada.
func TestConfirmarPackIncompleto(t *testing.T) {
	requiereMySQL(t)
	g := retenerOFallar(t, solicitud("gabi", 1, nuevaOferta(t, 5), nuevaOferta(t, 5)))
	parcial := g.Retenciones[0].ID
	var noVigentes *models.ErrRetencionesNoVigentes
	if _, err := inv.Confirmar(ctx, uuid.NewString(), "gabi", []string{parcial}, lineasPara(g)); !errors.As(err, &noVigentes) {
		t.Fatalf("confirmar medio pack debería fallar: %v", err)
	}
	if _, err := inv.Confirmar(ctx, uuid.NewString(), "gabi", idsDe(g), lineasPara(g)); err != nil {
		t.Fatalf("el pack completo debería confirmarse: %v", err)
	}
}

// Liberar es idempotente y libera el cupo al instante (RN-11).
func TestLiberar(t *testing.T) {
	requiereMySQL(t)
	oferta := nuevaOferta(t, 1)
	g := retenerOFallar(t, solicitud("hugo", 1, oferta))
	id := g.Retenciones[0].ID
	for range 2 {
		if err := inv.Liberar(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := inv.Liberar(ctx, "no-existe"); err != nil {
		t.Fatalf("liberar algo inexistente no es un error: %v", err)
	}
	if e := estadoRetencion(t, id); e != models.RetencionLiberada {
		t.Fatalf("estado = %s", e)
	}
	if d := disponibilidad(t, oferta); d.Estado != models.Disponible || d.Libres != 1 {
		t.Fatalf("el cupo debería volver a estar libre: %+v", d)
	}
}

// El barrido marca VENCIDA y deja RetencionVencida y DisponibilidadCambiada en el outbox.
func TestVencerPendientes(t *testing.T) {
	requiereMySQL(t)
	oferta := nuevaOferta(t, 3)
	g := retenerOFallar(t, solicitud("ines", 1, oferta))
	vencer(t, g)

	n, err := inv.VencerPendientes(ctx, 100)
	if err != nil || n < 1 {
		t.Fatalf("vencidas = %d, err = %v", n, err)
	}
	if e := estadoRetencion(t, g.Retenciones[0].ID); e != models.RetencionVencida {
		t.Fatalf("estado = %s", e)
	}
	var eventos int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM outbox WHERE tipo = 'RetencionVencida' AND JSON_UNQUOTE(payload->'$.retencionId') = ?`,
		g.Retenciones[0].ID).Scan(&eventos); err != nil {
		t.Fatal(err)
	}
	if eventos != 1 {
		t.Fatalf("esperaba 1 RetencionVencida en el outbox, hay %d", eventos)
	}
	// Un segundo barrido no hace nada.
	if n, _ := inv.VencerPendientes(ctx, 100); n != 0 {
		t.Fatalf("el segundo barrido venció %d", n)
	}
}

// RN-01 en la edición: el cupo no puede bajar de lo vendido más lo retenido.
func TestActualizarCupoNoBajaDeLoOcupado(t *testing.T) {
	requiereMySQL(t)
	oferta := nuevaOferta(t, 10)
	retenerOFallar(t, solicitud("juan", 4, oferta))
	var regla *models.ErrRegla
	if err := inv.ActualizarCupo(ctx, oferta, 3); !errors.As(err, &regla) || regla.Codigo != models.CodigoCupoMenorAlOcupado {
		t.Fatalf("esperaba CUPO_MENOR_AL_OCUPADO: %v", err)
	}
	if err := inv.ActualizarCupo(ctx, oferta, 4); err != nil {
		t.Fatalf("cupo igual a lo ocupado es válido: %v", err)
	}
	if d := disponibilidad(t, oferta); d.Estado != models.Retenida {
		t.Fatalf("con 4 de 4 retenidos debería estar RETENIDA: %+v", d)
	}
}
