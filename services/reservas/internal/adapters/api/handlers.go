package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"agencia/pkg/observabilidad"
	"agencia/reservas/internal/application"
	"agencia/reservas/internal/domain"
	"agencia/reservas/internal/ports"
)

const reintentarEnSegundos = 5

// ---------- DTO de salida ----------

// importe sale como número JSON con 2 decimales, nunca como float ni como string.
func importe(d decimal.Decimal) json.Number { return json.Number(d.StringFixed(2)) }

type itemSalida struct {
	ID               string      `json:"id"`
	OfertaID         string      `json:"ofertaId,omitempty"`
	PackID           string      `json:"packId,omitempty"`
	Cantidad         int         `json:"cantidad"`
	Descripcion      string      `json:"descripcion"`
	Destino          string      `json:"destino"`
	PrecioReferencia json.Number `json:"precioReferencia"`
	ExpiraEn         time.Time   `json:"expiraEn"`
	Estado           string      `json:"estado"`
}

func aItemSalida(it domain.ItemCarrito) itemSalida {
	return itemSalida{ID: it.ID, OfertaID: it.OfertaID, PackID: it.PackID, Cantidad: it.Cantidad, Descripcion: it.Descripcion,
		Destino: it.Destino, PrecioReferencia: importe(it.PrecioReferencia), ExpiraEn: it.ExpiraEn, Estado: string(it.Estado)}
}

type lineaSalida struct {
	Descripcion string      `json:"descripcion"`
	Destino     string      `json:"destino"`
	Precio      json.Number `json:"precio"`
}

type compraSalida struct {
	ID                  string        `json:"id"`
	Estado              string        `json:"estado"`
	Total               json.Number   `json:"total"`
	Lineas              []lineaSalida `json:"lineas"`
	RetencionesVencidas []string      `json:"retencionesVencidas,omitempty"`
	CreadaEn            time.Time     `json:"creadaEn"`
}

func aCompraSalida(c *domain.Compra) compraSalida {
	s := compraSalida{ID: c.ID, Estado: string(c.Estado), Total: importe(c.Total), Lineas: []lineaSalida{},
		RetencionesVencidas: c.RetencionesVencidas, CreadaEn: c.CreadaEn}
	for _, l := range c.Lineas {
		s.Lineas = append(s.Lineas, lineaSalida{Descripcion: l.Descripcion, Destino: l.Destino, Precio: importe(l.Precio)})
	}
	return s
}

type guiaSalida struct {
	ID            string    `json:"id"`
	Titulo        string    `json:"titulo"`
	Destino       string    `json:"destino"`
	Etiquetas     []string  `json:"etiquetas"`
	Estado        string    `json:"estado"`
	MotivoRechazo string    `json:"motivoRechazo,omitempty"`
	CreadaEn      time.Time `json:"creadaEn"`
}

func aGuiaSalida(g domain.Guia) guiaSalida {
	etiquetas := g.Etiquetas
	if etiquetas == nil {
		etiquetas = []string{}
	}
	return guiaSalida{ID: g.ID, Titulo: g.Titulo, Destino: g.Destino, Etiquetas: etiquetas, Estado: string(g.Estado),
		MotivoRechazo: g.MotivoRechazo, CreadaEn: g.CreadaEn}
}

// ---------- Carrito ----------

func verCarrito(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		carrito, err := d.Carritos.Ver(c.Request.Context(), c.GetString(claveUsuario))
		if err != nil {
			noDisponible(c, d, err)
			return
		}
		items := make([]itemSalida, len(carrito.Items))
		for i, it := range carrito.Items {
			items[i] = aItemSalida(it)
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "totalReferencia": importe(carrito.TotalReferencia())})
	}
}

type solicitudAgregar struct {
	OfertaID string   `json:"ofertaId"`
	Cantidad int      `json:"cantidad"`
	PackID   string   `json:"packId"`
	Opciones []string `json:"opciones"`
}

func agregarAlCarrito(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		var s solicitudAgregar
		dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&s); err != nil {
			invalida(c, "", "el cuerpo debe ser un JSON con ofertaId (o packId y opciones) y cantidad")
			return
		}
		clave := c.GetHeader(headerIdempotencia)
		if clave != "" && !claveValida(clave) {
			invalida(c, headerIdempotencia, "debe tener entre 8 y 64 caracteres")
			return
		}
		item, existia, err := d.Carritos.Agregar(c.Request.Context(), application.SolicitudAgregar{
			UsuarioID: c.GetString(claveUsuario), IdempotencyKey: clave,
			OfertaID: s.OfertaID, Cantidad: s.Cantidad, PackID: s.PackID, Opciones: s.Opciones,
		})
		var (
			inv     *application.ErrSolicitudInvalida
			sinCupo *ports.ErrSinCupo
			regla   *ports.ErrReglaCatalogo
		)
		switch {
		case err == nil:
		case errors.As(err, &inv):
			invalida(c, inv.Campo, inv.Mensaje)
			return
		case errors.As(err, &sinCupo):
			extra := gin.H{"estado": sinCupo.Estado, "ofertaId": sinCupo.OfertaID}
			if sinCupo.ProximaLiberacion != nil {
				extra["proximaLiberacion"] = sinCupo.ProximaLiberacion
			}
			detalle := "No queda cupo: está agotada."
			if sinCupo.Estado == "RETENIDA" {
				detalle = "Los cupos libres están retenidos en otros carritos; pueden liberarse en unos minutos."
			}
			problema(c, http.StatusConflict, "sin-cupo", "Sin cupo", detalle, extra)
			return
		case errors.As(err, &regla):
			problema(c, http.StatusUnprocessableEntity, "regla-incumplida", "Regla incumplida", regla.Detalle, gin.H{"motivo": regla.Codigo})
			return
		case errors.Is(err, ports.ErrCatalogoNoDisponible):
			c.Header("Retry-After", strconv.Itoa(reintentarEnSegundos))
			logError(c, d, "no se pudo retener el cupo", err)
			problema(c, http.StatusServiceUnavailable, "no-disponible", "No pudimos reservar tu lugar",
				"Reintentá en unos minutos; el ítem no se agregó.")
			return
		default:
			noDisponible(c, d, err)
			return
		}
		estado := http.StatusCreated
		if existia {
			estado = http.StatusOK
		}
		c.JSON(estado, aItemSalida(item))
	}
}

func quitarDelCarrito(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		liberado, err := d.Carritos.Quitar(c.Request.Context(), c.GetString(claveUsuario), c.Param("itemId"))
		switch {
		case errors.Is(err, domain.ErrItemNoEncontrado):
			problema(c, http.StatusNotFound, "no-encontrado", "Ítem no encontrado", err.Error())
		case err != nil:
			noDisponible(c, d, err)
		default:
			if !liberado {
				observabilidad.Logger(c.Request.Context(), d.Log).Warn("ítem quitado sin liberar el cupo en Catálogo: la retención vencerá sola",
					"item_id", c.Param("itemId"))
			}
			c.Status(http.StatusNoContent)
		}
	}
}

// ---------- Compras ----------

func comprar(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		clave := c.GetHeader(headerIdempotencia)
		if !claveValida(clave) {
			invalida(c, headerIdempotencia, "es obligatoria (una por intento de compra) y debe tener entre 8 y 64 caracteres")
			return
		}
		email := c.GetString(claveEmail)
		if email == "" {
			problema(c, http.StatusUnauthorized, "no-autenticado", "No autenticado", "Falta el email del usuario.")
			return
		}
		compra, yaExistia, err := d.Compras.Comprar(c.Request.Context(), c.GetString(claveUsuario), email, clave)
		switch {
		case errors.Is(err, domain.ErrCarritoVacio):
			problema(c, http.StatusUnprocessableEntity, "carrito-vacio", "Carrito vacío", err.Error())
			return
		case err != nil:
			noDisponible(c, d, err)
			return
		}
		salida := aCompraSalida(compra)
		switch {
		case yaExistia:
			c.JSON(http.StatusOK, salida)
		case compra.Estado == domain.CompraConfirmada:
			c.Header("Location", "/api/compras/"+compra.ID)
			c.JSON(http.StatusCreated, salida)
		case compra.Estado == domain.CompraRechazada:
			c.JSON(http.StatusConflict, salida)
		default: // PENDIENTE: el cliente consulta GET /api/compras/{id}
			c.Header("Location", "/api/compras/"+compra.ID)
			c.Header("Retry-After", strconv.Itoa(reintentarEnSegundos))
			c.JSON(http.StatusAccepted, salida)
		}
	}
}

func listarCompras(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		compras, err := d.Compras.Listar(c.Request.Context(), c.GetString(claveUsuario))
		if err != nil {
			noDisponible(c, d, err)
			return
		}
		res := make([]compraSalida, len(compras))
		for i := range compras {
			res[i] = aCompraSalida(&compras[i])
		}
		c.JSON(http.StatusOK, gin.H{"compras": res})
	}
}

func obtenerCompra(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		compra, err := d.Compras.Obtener(c.Request.Context(), c.GetString(claveUsuario), c.Param("compraId"))
		switch {
		case errors.Is(err, ports.ErrNoEncontrado):
			problema(c, http.StatusNotFound, "no-encontrado", "Compra no encontrada", "No existe una compra con ese id para tu usuario.")
		case err != nil:
			noDisponible(c, d, err)
		default:
			c.JSON(http.StatusOK, aCompraSalida(compra))
		}
	}
}

// ---------- Guías (HU-13) ----------

func compartirGuia(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, domain.MaxTamanioPDF+(1<<20))
		archivo, err := c.FormFile("archivo")
		if err != nil {
			var grande *http.MaxBytesError
			if errors.As(err, &grande) {
				problema(c, http.StatusRequestEntityTooLarge, "archivo-grande", "Archivo demasiado grande", "El PDF puede pesar hasta 10 MB.")
				return
			}
			invalida(c, "archivo", "falta el PDF de la guía (campo archivo)")
			return
		}
		f, err := archivo.Open()
		if err != nil {
			invalida(c, "archivo", "no se pudo leer el archivo")
			return
		}
		defer f.Close()
		pdf, err := io.ReadAll(io.LimitReader(f, domain.MaxTamanioPDF+1))
		if err != nil {
			invalida(c, "archivo", "no se pudo leer el archivo")
			return
		}
		// Texto que no es UTF-8 (por ejemplo, de una terminal en Windows-1252) se
		// rechaza acá: si llegara a MySQL fallaría como si el servicio estuviera caído.
		for _, campo := range []string{"titulo", "descripcion", "destino", "autor", "etiquetas"} {
			if !utf8.ValidString(c.PostForm(campo)) {
				invalida(c, campo, "no es texto UTF-8 válido")
				return
			}
		}
		var etiquetas []string
		if e := c.PostForm("etiquetas"); e != "" {
			etiquetas = strings.Split(e, ",")
		}
		descripcion := c.PostForm("descripcion")

		g, err := d.Guias.Compartir(c.Request.Context(), c.GetString(claveUsuario), domain.SolicitudGuia{
			Titulo: c.PostForm("titulo"), Descripcion: descripcion, Destino: c.PostForm("destino"),
			Etiquetas: etiquetas, Autor: c.PostForm("autor"), PDF: pdf,
		})
		var val *domain.ErrValidacionGuia
		switch {
		case errors.As(err, &val):
			invalida(c, val.Campo, val.Mensaje)
		case errors.Is(err, domain.ErrPDFInvalido):
			problema(c, http.StatusUnsupportedMediaType, "archivo-invalido", "Archivo inválido", err.Error())
		case errors.Is(err, domain.ErrDestinoNoComprado):
			problema(c, http.StatusForbidden, "destino-no-comprado", "Destino no comprado", err.Error())
		case err != nil:
			noDisponible(c, d, err)
		default:
			c.JSON(http.StatusAccepted, aGuiaSalida(*g))
		}
	}
}

func listarGuias(d Dependencias) gin.HandlerFunc {
	return func(c *gin.Context) {
		guias, err := d.Guias.Listar(c.Request.Context(), c.GetString(claveUsuario))
		if err != nil {
			noDisponible(c, d, err)
			return
		}
		res := make([]guiaSalida, len(guias))
		for i, g := range guias {
			res[i] = aGuiaSalida(g)
		}
		c.JSON(http.StatusOK, gin.H{"guias": res})
	}
}

// ---------- auxiliares ----------

func claveValida(clave string) bool {
	n := utf8.RuneCountInString(clave)
	return n >= 8 && n <= 64
}

func invalida(c *gin.Context, campo, mensaje string) {
	problema(c, http.StatusBadRequest, "solicitud-invalida", "Solicitud inválida", "",
		gin.H{"errores": []gin.H{{"campo": campo, "mensaje": mensaje}}})
}

func logError(c *gin.Context, d Dependencias, msg string, err error) {
	ctx := c.Request.Context()
	observabilidad.Logger(ctx, d.Log).ErrorContext(ctx, msg, "error", err)
}

func noDisponible(c *gin.Context, d Dependencias, err error) {
	logError(c, d, "no se pudo atender la solicitud", err)
	c.Header("Retry-After", strconv.Itoa(reintentarEnSegundos))
	problema(c, http.StatusServiceUnavailable, "no-disponible", "Servicio no disponible", "Reintentá en unos segundos.")
}
