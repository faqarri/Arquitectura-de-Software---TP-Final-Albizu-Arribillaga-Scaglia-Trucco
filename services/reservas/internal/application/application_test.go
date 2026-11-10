package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

var ctx = context.Background()

type entorno struct {
	mem      *memoria
	catalogo *catalogoFalso
	externas *externasFalsas
	carritos *Carritos
	compras  *Compras
	guias    *Guias
}

func nuevoEntorno() entorno {
	mem := nuevaMemoria()
	cat := nuevoCatalogo()
	ext := &externasFalsas{}
	return entorno{
		mem: mem, catalogo: cat, externas: ext,
		carritos: NuevoCarritos(mem, cat),
		compras:  NuevoCompras(comprasMem{mem}, mem, cat),
		guias:    NuevoGuias(guiasMem{mem}, comprasMem{mem}, ext),
	}
}

func (e entorno) agregar(t *testing.T, clave string) domain.ItemCarrito {
	t.Helper()
	it, _, err := e.carritos.Agregar(ctx, SolicitudAgregar{UsuarioID: "u1", IdempotencyKey: clave, OfertaID: "of-1", Cantidad: 2})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

// ---------- Carrito ----------

func TestAgregarRetieneYGuardaElItem(t *testing.T) {
	e := nuevoEntorno()
	it := e.agregar(t, "clave-1")
	if len(it.Retenciones) != 1 || it.Destino != "Bariloche" || it.PrecioReferencia.IntPart() != 2000 || it.Estado != domain.ItemActivo {
		t.Errorf("ítem = %+v", it)
	}
	c, _ := e.carritos.Ver(ctx, "u1")
	if len(c.Items) != 1 {
		t.Errorf("carrito = %+v", c)
	}
}

func TestAgregarConLaMismaClaveNoDuplica(t *testing.T) {
	e := nuevoEntorno()
	primero := e.agregar(t, "clave-1")
	segundo, existia, err := e.carritos.Agregar(ctx, SolicitudAgregar{UsuarioID: "u1", IdempotencyKey: "clave-1", OfertaID: "of-1", Cantidad: 2})
	if err != nil || !existia || segundo.ID != primero.ID {
		t.Fatalf("segundo=%+v existia=%v err=%v", segundo, existia, err)
	}
	if c, _ := e.carritos.Ver(ctx, "u1"); len(c.Items) != 1 {
		t.Errorf("se duplicó el ítem: %d", len(c.Items))
	}
}

func TestAgregarValidaLaSolicitud(t *testing.T) {
	e := nuevoEntorno()
	casos := []SolicitudAgregar{
		{UsuarioID: "u1"}, // ni oferta ni pack
		{UsuarioID: "u1", OfertaID: "o", PackID: "p"},               // las dos
		{UsuarioID: "u1", PackID: "p"},                              // pack sin opciones
		{UsuarioID: "u1", OfertaID: "o", Cantidad: MaxCantidad + 1}, // RN-12
	}
	for _, s := range casos {
		var inv *ErrSolicitudInvalida
		if _, _, err := e.carritos.Agregar(ctx, s); !errors.As(err, &inv) {
			t.Errorf("%+v: err = %v", s, err)
		}
	}
}

func TestAgregarSinCupoDevuelveElErrorDeCatalogo(t *testing.T) {
	e := nuevoEntorno()
	e.catalogo.errRetener = &ports.ErrSinCupo{Estado: "RETENIDA"}
	var sinCupo *ports.ErrSinCupo
	if _, _, err := e.carritos.Agregar(ctx, SolicitudAgregar{UsuarioID: "u1", OfertaID: "o"}); !errors.As(err, &sinCupo) {
		t.Fatalf("err = %v", err)
	}
	if c, _ := e.carritos.Ver(ctx, "u1"); !c.Vacio() {
		t.Error("no debía quedar nada en el carrito")
	}
}

func TestSiNoSePuedeGuardarSeLiberaLaRetencion(t *testing.T) {
	e := nuevoEntorno()
	e.mem.fallar = errors.New("base caída")
	if _, _, err := e.carritos.Agregar(ctx, SolicitudAgregar{UsuarioID: "u1", OfertaID: "o"}); err == nil {
		t.Fatal("se esperaba error")
	}
	if len(e.catalogo.liberadas) != 1 {
		t.Error("debía liberar el cupo retenido")
	}
}

func TestQuitarLiberaElCupo(t *testing.T) {
	e := nuevoEntorno()
	it := e.agregar(t, "clave-1")
	liberado, err := e.carritos.Quitar(ctx, "u1", it.ID)
	if err != nil || !liberado || len(e.catalogo.liberadas) != 1 {
		t.Fatalf("liberado=%v err=%v", liberado, err)
	}
	if _, err := e.carritos.Quitar(ctx, "u1", it.ID); !errors.Is(err, domain.ErrItemNoEncontrado) {
		t.Errorf("err = %v", err)
	}
}

func TestQuitarConCatalogoCaidoQuitaIgual(t *testing.T) {
	e := nuevoEntorno()
	it := e.agregar(t, "clave-1")
	e.catalogo.errRetener = ports.ErrCatalogoNoDisponible
	liberado, err := e.carritos.Quitar(ctx, "u1", it.ID)
	if err != nil || liberado {
		t.Fatalf("liberado=%v err=%v", liberado, err)
	}
	if c, _ := e.carritos.Ver(ctx, "u1"); !c.Vacio() {
		t.Error("el ítem debía quitarse aunque Catálogo no responda")
	}
}

func TestOtroUsuarioNoPuedeQuitarMiItem(t *testing.T) {
	e := nuevoEntorno()
	it := e.agregar(t, "clave-1")
	if _, err := e.carritos.Quitar(ctx, "u2", it.ID); !errors.Is(err, domain.ErrItemNoEncontrado) {
		t.Errorf("err = %v", err)
	}
}

func TestRetencionVencidaMarcaElItem(t *testing.T) {
	e := nuevoEntorno()
	it := e.agregar(t, "clave-1")
	if err := e.carritos.MarcarRetencionVencida(ctx, it.Retenciones[0]); err != nil {
		t.Fatal(err)
	}
	c, _ := e.carritos.Ver(ctx, "u1")
	if c.Items[0].Estado != domain.ItemVencido {
		t.Errorf("estado = %s", c.Items[0].Estado)
	}
	if err := e.carritos.MarcarRetencionVencida(ctx, "inexistente"); err != nil {
		t.Errorf("un evento de una retención desconocida no es un error: %v", err)
	}
}

// ---------- Compras ----------

func TestComprarConfirmaPublicaYVaciaElCarrito(t *testing.T) {
	e := nuevoEntorno()
	e.agregar(t, "clave-1")
	c, existia, err := e.compras.Comprar(ctx, "u1", "ana@example.com", "compra-1")
	if err != nil || existia {
		t.Fatalf("err=%v existia=%v", err, existia)
	}
	if c.Estado != domain.CompraConfirmada || c.Total.IntPart() != 1000 || len(c.Lineas) != 1 {
		t.Errorf("compra = %+v", c)
	}
	if len(e.mem.outbox) != 1 || e.mem.outbox[0] != c.ID {
		t.Errorf("outbox = %v; se esperaba un CompraConfirmada", e.mem.outbox)
	}
	if carrito, _ := e.carritos.Ver(ctx, "u1"); !carrito.Vacio() {
		t.Error("el carrito debía quedar vacío")
	}
}

func TestComprarDosVecesConLaMismaClaveNoDuplica(t *testing.T) {
	e := nuevoEntorno()
	e.agregar(t, "clave-1")
	primera, _, _ := e.compras.Comprar(ctx, "u1", "ana@example.com", "compra-1")
	segunda, existia, err := e.compras.Comprar(ctx, "u1", "ana@example.com", "compra-1")
	if err != nil || !existia || segunda.ID != primera.ID {
		t.Fatalf("segunda=%+v existia=%v err=%v", segunda, existia, err)
	}
	if len(e.mem.outbox) != 1 || len(e.catalogo.confirmadas) != 1 {
		t.Errorf("outbox=%v confirmadas=%v", e.mem.outbox, e.catalogo.confirmadas)
	}
}

func TestComprarConRetencionVencidaRechazaYMarcaElItem(t *testing.T) {
	e := nuevoEntorno()
	it := e.agregar(t, "clave-1")
	e.catalogo.vencidas = it.Retenciones
	c, _, err := e.compras.Comprar(ctx, "u1", "ana@example.com", "compra-1")
	if err != nil || c.Estado != domain.CompraRechazada || c.RetencionesVencidas[0] != it.Retenciones[0] {
		t.Fatalf("c=%+v err=%v", c, err)
	}
	if len(e.mem.outbox) != 0 {
		t.Error("una compra rechazada no publica CompraConfirmada")
	}
	carrito, _ := e.carritos.Ver(ctx, "u1")
	if carrito.Items[0].Estado != domain.ItemVencido {
		t.Error("el ítem vencido debía quedar marcado")
	}
}

func TestComprarConCatalogoCaidoQuedaPendienteYSeRecupera(t *testing.T) {
	e := nuevoEntorno()
	e.agregar(t, "clave-1")
	e.catalogo.errConfirm = ports.ErrCatalogoNoDisponible
	c, _, err := e.compras.Comprar(ctx, "u1", "ana@example.com", "compra-1")
	if err != nil || c.Estado != domain.CompraPendiente {
		t.Fatalf("c=%+v err=%v", c, err)
	}

	// La recuperación no toma compras recién creadas.
	if n, _ := e.compras.RecuperarPendientes(ctx, 10); n != 0 {
		t.Errorf("resueltas = %d", n)
	}
	e.compras.ahora = func() time.Time { return time.Now().Add(EsperaRecuperacion + time.Second) }
	e.catalogo.errConfirm = nil
	n, err := e.compras.RecuperarPendientes(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	final, _ := e.compras.Obtener(ctx, "u1", c.ID)
	if final.Estado != domain.CompraConfirmada || len(e.mem.outbox) != 1 {
		t.Errorf("final=%+v outbox=%v", final, e.mem.outbox)
	}
	if e.catalogo.confirmadas[0] != c.ID {
		t.Error("la recuperación debe usar el id de la compra como clave")
	}
}

func TestComprarCarritoVacio(t *testing.T) {
	e := nuevoEntorno()
	if _, _, err := e.compras.Comprar(ctx, "u1", "ana@example.com", "c"); !errors.Is(err, domain.ErrCarritoVacio) {
		t.Errorf("err = %v", err)
	}
}

// ---------- Guías ----------

var pdf = []byte("%PDF-1.4 guía")

func (e entorno) comprarBariloche(t *testing.T) {
	t.Helper()
	e.agregar(t, "clave-guia")
	if c, _, err := e.compras.Comprar(ctx, "u1", "ana@example.com", "compra-guia"); err != nil || c.Estado != domain.CompraConfirmada {
		t.Fatalf("c=%+v err=%v", c, err)
	}
}

func solicitudGuia(destino string) domain.SolicitudGuia {
	return domain.SolicitudGuia{Titulo: "Trekking en el Catedral", Destino: destino, Autor: "Ana", PDF: pdf}
}

func TestCompartirGuiaDeDestinoComprado(t *testing.T) {
	e := nuevoEntorno()
	e.comprarBariloche(t)
	g, err := e.guias.Compartir(ctx, "u1", solicitudGuia("bariloche"))
	if err != nil {
		t.Fatal(err)
	}
	if g.Estado != domain.GuiaPendiente || g.DocumentoID == "" || len(e.externas.registros) != 1 {
		t.Errorf("g = %+v", g)
	}
}

func TestNoSePuedeCompartirGuiaDeUnDestinoNoComprado(t *testing.T) {
	e := nuevoEntorno()
	e.comprarBariloche(t)
	if _, err := e.guias.Compartir(ctx, "u1", solicitudGuia("Mendoza")); !errors.Is(err, domain.ErrDestinoNoComprado) {
		t.Errorf("err = %v", err)
	}
	if _, err := e.guias.Compartir(ctx, "u2", solicitudGuia("Bariloche")); !errors.Is(err, domain.ErrDestinoNoComprado) {
		t.Errorf("otro usuario: err = %v", err)
	}
}

func TestConEstudiarioCaidoLaGuiaQuedaPorEnviarYSeEnviaDespues(t *testing.T) {
	e := nuevoEntorno()
	e.comprarBariloche(t)
	e.externas.err = ports.ErrGuiasNoDisponible
	g, err := e.guias.Compartir(ctx, "u1", solicitudGuia("Bariloche"))
	if err != nil || g.Estado != domain.GuiaPorEnviar {
		t.Fatalf("g=%+v err=%v; la falla del proveedor no le llega al cliente", g, err)
	}

	if _, err := e.guias.EnviarPendientes(ctx, 10); !errors.Is(err, ports.ErrGuiasNoDisponible) {
		t.Errorf("err = %v", err)
	}
	e.externas.err = nil
	n, err := e.guias.EnviarPendientes(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	guias, _ := e.guias.Listar(ctx, "u1")
	if guias[0].Estado != domain.GuiaPendiente || guias[0].Intentos != 2 {
		t.Errorf("guía = %+v", guias[0])
	}
	if e.externas.registros[0] != g.IdempotencyKey {
		t.Error("el reenvío debe usar la misma Idempotency-Key")
	}
}

func TestCompartirNoEsperaMasDelLimiteSiEstudiarioNoResponde(t *testing.T) {
	e := nuevoEntorno()
	e.comprarBariloche(t)
	e.externas.colgado = true
	e.guias.esperaEnvioInmediato = 50 * time.Millisecond
	inicio := time.Now()
	g, err := e.guias.Compartir(ctx, "u1", solicitudGuia("Bariloche"))
	if err != nil || g.Estado != domain.GuiaPorEnviar {
		t.Fatalf("g=%+v err=%v", g, err)
	}
	if d := time.Since(inicio); d > time.Second {
		t.Errorf("Compartir tardó %v: debía responder al vencer la espera", d)
	}
	if EsperaEnvioInmediato >= 15*time.Second {
		t.Error("la espera tiene que quedar por debajo del timeout del gateway (15 s)")
	}
}

func TestGuiaRechazadaPorElProveedor(t *testing.T) {
	e := nuevoEntorno()
	e.comprarBariloche(t)
	e.externas.err = &ports.ErrGuiaRechazada{Motivo: "title: muy corto"}
	g, err := e.guias.Compartir(ctx, "u1", solicitudGuia("Bariloche"))
	if err != nil || g.Estado != domain.GuiaRechazada || g.MotivoRechazo != "title: muy corto" {
		t.Fatalf("g=%+v err=%v", g, err)
	}
}

func TestActualizarModeracion(t *testing.T) {
	e := nuevoEntorno()
	e.comprarBariloche(t)
	g, _ := e.guias.Compartir(ctx, "u1", solicitudGuia("Bariloche"))
	e.externas.moderacion = []ports.EstadoModeracion{{DocumentoID: g.DocumentoID, Rechazada: true, Motivo: "duplicada"}}

	n, err := e.guias.ActualizarModeracion(ctx)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	guias, _ := e.guias.Listar(ctx, "u1")
	if guias[0].Estado != domain.GuiaRechazada || guias[0].MotivoRechazo != "duplicada" {
		t.Errorf("guía = %+v", guias[0])
	}
}
