package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"agencia/catalogo/internal/models"
)

// Inventario guarda los cupos de cada oferta y sus retenciones (ADR-014).
//
// Regla de bloqueo, que vale para retener, liberar, confirmar y vencer: toda
// operación que cambia retenciones o el inventario de una oferta bloquea
// primero las filas de inventario involucradas, en orden ascendente de
// oferta_id, y recién después toca retenciones. Así las operaciones sobre una
// misma oferta se ejecutan de a una y nunca se bloquean mutuamente.
type Inventario struct {
	db *sql.DB
}

func NuevoInventario(db *sql.DB) *Inventario { return &Inventario{db: db} }

// querier es lo que comparten *sql.DB y *sql.Tx.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type filaInventario struct {
	cupoTotal int
	vendidos  int
	version   int64
}

// bloquearInventario toma FOR UPDATE las filas de inventario de las ofertas, en
// orden ascendente de oferta_id. Las ofertas sin fila no aparecen en el mapa.
func bloquearInventario(ctx context.Context, tx *sql.Tx, ofertaIDs []string) (map[string]*filaInventario, error) {
	ids := ordenadosSinRepetir(ofertaIDs)
	res := make(map[string]*filaInventario, len(ids))
	if len(ids) == 0 {
		return res, nil
	}
	in, args := marcadores(ids)
	filas, err := tx.QueryContext(ctx,
		`SELECT oferta_id, cupo_total, vendidos, version FROM inventario
		 WHERE oferta_id IN (`+in+`) ORDER BY oferta_id FOR UPDATE`, args...)
	if err != nil {
		return nil, fmt.Errorf("bloquear inventario: %w", err)
	}
	defer filas.Close()
	for filas.Next() {
		var id string
		f := &filaInventario{}
		if err := filas.Scan(&id, &f.cupoTotal, &f.vendidos, &f.version); err != nil {
			return nil, err
		}
		res[id] = f
	}
	return res, filas.Err()
}

// ocupacion suma los cupos retenidos por retenciones vigentes (ACTIVA y sin
// vencer según el reloj de la base, RN-11) y devuelve el menor vencimiento.
func ocupacion(ctx context.Context, q querier, ofertaID string) (retenidos int, proxima *time.Time, err error) {
	var prox sql.NullTime
	err = q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cantidad), 0), MIN(expira_en) FROM retenciones
		 WHERE oferta_id = ? AND estado = 'ACTIVA' AND expira_en > NOW(6)`, ofertaID).Scan(&retenidos, &prox)
	if err != nil {
		return 0, nil, fmt.Errorf("calcular ocupación de %s: %w", ofertaID, err)
	}
	if prox.Valid {
		t := prox.Time.UTC()
		proxima = &t
	}
	return retenidos, proxima, nil
}

// ---------- Alta y edición del cupo (las usa la administración de ofertas) ----------

// CrearInventario registra el cupo de una oferta nueva.
func (r *Inventario) CrearInventario(ctx context.Context, ofertaID string, cupoTotal int) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO inventario (oferta_id, cupo_total) VALUES (?, ?)`, ofertaID, cupoTotal)
	if err != nil {
		return fmt.Errorf("crear inventario de %s: %w", ofertaID, err)
	}
	return nil
}

// ActualizarCupo cambia el cupo total. No puede quedar por debajo de lo ya
// ocupado (vendidos más retenidos vigentes), porque violaría RN-01. Si la
// oferta no tenía inventario (por ejemplo, cargada a mano en MongoDB), lo crea.
func (r *Inventario) ActualizarCupo(ctx context.Context, ofertaID string, cupoTotal int) error {
	headers := headersDe(ctx)
	return enTransaccion(ctx, r.db, func(tx *sql.Tx) error {
		inv, err := bloquearInventario(ctx, tx, []string{ofertaID})
		if err != nil {
			return err
		}
		if inv[ofertaID] == nil {
			_, err := tx.ExecContext(ctx, `INSERT INTO inventario (oferta_id, cupo_total) VALUES (?, ?)`, ofertaID, cupoTotal)
			return err
		}
		retenidos, _, err := ocupacion(ctx, tx, ofertaID)
		if err != nil {
			return err
		}
		if ocupado := inv[ofertaID].vendidos + retenidos; cupoTotal < ocupado {
			return &models.ErrRegla{
				Codigo:  models.CodigoCupoMenorAlOcupado,
				Detalle: fmt.Sprintf("El cupo no puede ser menor que los %d lugares vendidos o reservados.", ocupado),
			}
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE inventario SET cupo_total = ?, version = version + 1 WHERE oferta_id = ?`, cupoTotal, ofertaID); err != nil {
			return err
		}
		return registrarDisponibilidad(ctx, tx, ofertaID, headers)
	})
}

// Cupo devuelve el cupo total y los vendidos de una oferta (para el administrador).
func (r *Inventario) Cupo(ctx context.Context, ofertaID string) (cupoTotal, vendidos int, err error) {
	err = r.db.QueryRowContext(ctx, `SELECT cupo_total, vendidos FROM inventario WHERE oferta_id = ?`, ofertaID).
		Scan(&cupoTotal, &vendidos)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, models.ErrNoEncontrado
	}
	return cupoTotal, vendidos, err
}

// Disponibilidad calcula el estado actual de una oferta (RN-13). Es una lectura
// sin bloqueos: siempre sale de MySQL, nunca de la caché (ADR-007).
func (r *Inventario) Disponibilidad(ctx context.Context, ofertaID string) (models.Disponibilidad, error) {
	var f filaInventario
	err := r.db.QueryRowContext(ctx, `SELECT cupo_total, vendidos, version FROM inventario WHERE oferta_id = ?`, ofertaID).
		Scan(&f.cupoTotal, &f.vendidos, &f.version)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Disponibilidad{}, models.ErrNoEncontrado
	}
	if err != nil {
		return models.Disponibilidad{}, fmt.Errorf("leer inventario: %w", err)
	}
	retenidos, proxima, err := ocupacion(ctx, r.db, ofertaID)
	if err != nil {
		return models.Disponibilidad{}, err
	}
	return models.CalcularDisponibilidad(ofertaID, f.cupoTotal, f.vendidos, retenidos, proxima, f.version), nil
}

// ---------- Retener ----------

// ItemRetencion es una oferta a retener, con los datos que se le informan a
// Reservas para mostrar el carrito sin volver a consultar a Catálogo.
type ItemRetencion struct {
	OfertaID         string
	Cantidad         int
	Descripcion      string
	Destino          string
	PrecioReferencia float64
}

// SolicitudRetencion: una oferta suelta (un ítem) o las opciones de un pack.
type SolicitudRetencion struct {
	IdempotencyKey string
	TitularID      string
	PackID         string
	Items          []ItemRetencion
	Minutos        int
	MaxPorOferta   int
}

var errGrupoDuplicado = errors.New("ya existe un grupo con esa Idempotency-Key")

// Retener crea las retenciones de la solicitud en una sola transacción: todas o
// ninguna (RN-07). Un reintento con la misma Idempotency-Key devuelve el grupo
// ya creado, aunque ya no esté vigente.
func (r *Inventario) Retener(ctx context.Context, s SolicitudRetencion) (models.GrupoRetenido, error) {
	if g, ok, err := r.grupoPorClave(ctx, s.IdempotencyKey); err != nil || ok {
		return g, err
	}
	headers := headersDe(ctx)
	items := slices.Clone(s.Items)
	slices.SortFunc(items, func(a, b ItemRetencion) int { return compararTexto(a.OfertaID, b.OfertaID) })

	var grupo models.GrupoRetenido
	err := enTransaccion(ctx, r.db, func(tx *sql.Tx) error {
		ofertaIDs := make([]string, len(items))
		for i, it := range items {
			ofertaIDs[i] = it.OfertaID
		}
		inv, err := bloquearInventario(ctx, tx, ofertaIDs)
		if err != nil {
			return err
		}
		for _, it := range items {
			if inv[it.OfertaID] == nil {
				return &models.ErrOfertaInexistente{ID: it.OfertaID}
			}
		}

		// Con el bloqueo tomado, se calcula lo libre de cada oferta (ADR-014).
		for _, it := range items {
			retenidos, proxima, err := ocupacion(ctx, tx, it.OfertaID)
			if err != nil {
				return err
			}
			var delTitular int
			if err := tx.QueryRowContext(ctx,
				`SELECT COALESCE(SUM(cantidad), 0) FROM retenciones
				 WHERE titular_id = ? AND oferta_id = ? AND estado = 'ACTIVA' AND expira_en > NOW(6)`,
				s.TitularID, it.OfertaID).Scan(&delTitular); err != nil {
				return fmt.Errorf("leer retenciones del titular: %w", err)
			}
			f := inv[it.OfertaID]
			if err := models.VerificarCupo(it.OfertaID, it.Cantidad, f.cupoTotal, f.vendidos, retenidos, delTitular, s.MaxPorOferta, proxima); err != nil {
				return err
			}
		}

		// Todas las retenciones del grupo vencen juntas, con el reloj de la base.
		var expira time.Time
		if err := tx.QueryRowContext(ctx, `SELECT NOW(6) + INTERVAL ? MINUTE`, s.Minutos).Scan(&expira); err != nil {
			return fmt.Errorf("calcular vencimiento: %w", err)
		}
		grupo = models.GrupoRetenido{GrupoID: uuid.NewString(), ExpiraEn: expira.UTC()}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO grupos_retencion (id, idempotency_key, titular_id, pack_id, expira_en) VALUES (?, ?, ?, ?, ?)`,
			grupo.GrupoID, s.IdempotencyKey, s.TitularID, nulo(s.PackID), expira)
		if esErrorMySQL(err, errClaveDuplicada) {
			return errGrupoDuplicado // otra solicitud con la misma clave ganó la carrera
		}
		if err != nil {
			return fmt.Errorf("registrar grupo: %w", err)
		}
		for _, it := range items {
			ret := models.RetencionCreada{
				ID: uuid.NewString(), OfertaID: it.OfertaID, Cantidad: it.Cantidad,
				Descripcion: it.Descripcion, Destino: it.Destino, PrecioReferencia: models.Redondear(it.PrecioReferencia),
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO retenciones (id, grupo_id, oferta_id, cantidad, titular_id, estado, expira_en, pack_id,
				                          descripcion, destino, precio_referencia)
				 VALUES (?, ?, ?, ?, ?, 'ACTIVA', ?, ?, ?, ?, ?)`,
				ret.ID, grupo.GrupoID, ret.OfertaID, ret.Cantidad, s.TitularID, expira, nulo(s.PackID),
				truncar(ret.Descripcion, 300), truncar(ret.Destino, 120), ret.PrecioReferencia); err != nil {
				return fmt.Errorf("registrar retención: %w", err)
			}
			grupo.Retenciones = append(grupo.Retenciones, ret)
		}
		for _, id := range ordenadosSinRepetir(ofertaIDs) {
			if err := incrementarVersion(ctx, tx, id); err != nil {
				return err
			}
			if err := registrarDisponibilidad(ctx, tx, id, headers); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errGrupoDuplicado) {
		g, _, err := r.grupoPorClave(ctx, s.IdempotencyKey)
		return g, err
	}
	return grupo, err
}

func (r *Inventario) grupoPorClave(ctx context.Context, clave string) (models.GrupoRetenido, bool, error) {
	var g models.GrupoRetenido
	err := r.db.QueryRowContext(ctx, `SELECT id, expira_en FROM grupos_retencion WHERE idempotency_key = ?`, clave).
		Scan(&g.GrupoID, &g.ExpiraEn)
	if errors.Is(err, sql.ErrNoRows) {
		return g, false, nil
	}
	if err != nil {
		return g, false, fmt.Errorf("buscar grupo por clave: %w", err)
	}
	g.ExpiraEn = g.ExpiraEn.UTC()
	filas, err := r.db.QueryContext(ctx,
		`SELECT id, oferta_id, cantidad, descripcion, destino, precio_referencia FROM retenciones
		 WHERE grupo_id = ? ORDER BY oferta_id`, g.GrupoID)
	if err != nil {
		return g, false, fmt.Errorf("leer retenciones del grupo: %w", err)
	}
	defer filas.Close()
	for filas.Next() {
		var ret models.RetencionCreada
		if err := filas.Scan(&ret.ID, &ret.OfertaID, &ret.Cantidad, &ret.Descripcion, &ret.Destino, &ret.PrecioReferencia); err != nil {
			return g, false, err
		}
		g.Retenciones = append(g.Retenciones, ret)
	}
	return g, true, filas.Err()
}

// ---------- Liberar ----------

// Liberar pasa a LIBERADA la retención y todo su grupo (un pack se libera
// entero). Es idempotente: si no existe o ya no está vigente, no hace nada.
func (r *Inventario) Liberar(ctx context.Context, retencionID string) error {
	var grupoID string
	err := r.db.QueryRowContext(ctx, `SELECT grupo_id FROM retenciones WHERE id = ?`, retencionID).Scan(&grupoID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("buscar retención: %w", err)
	}
	ofertaIDs, err := columna(ctx, r.db, `SELECT DISTINCT oferta_id FROM retenciones WHERE grupo_id = ?`, grupoID)
	if err != nil {
		return err
	}
	headers := headersDe(ctx)
	return enTransaccion(ctx, r.db, func(tx *sql.Tx) error {
		if _, err := bloquearInventario(ctx, tx, ofertaIDs); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE retenciones SET estado = 'LIBERADA'
			 WHERE grupo_id = ? AND estado = 'ACTIVA' AND expira_en > NOW(6)`, grupoID)
		if err != nil {
			return fmt.Errorf("liberar retenciones: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		for _, id := range ordenadosSinRepetir(ofertaIDs) {
			if err := incrementarVersion(ctx, tx, id); err != nil {
				return err
			}
			if err := registrarDisponibilidad(ctx, tx, id, headers); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------- Confirmar ----------

// DatosRetencion es lo que necesita la capa de servicios para calcular los
// precios antes de confirmar. Ninguno de estos datos cambia después de retener.
type DatosRetencion struct {
	ID       string
	OfertaID string
	PackID   string
	Cantidad int
}

// DatosRetenciones lee las retenciones pedidas (sin bloquear). Las que no
// existen no aparecen.
func (r *Inventario) DatosRetenciones(ctx context.Context, ids []string) ([]DatosRetencion, error) {
	ids = ordenadosSinRepetir(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	in, args := marcadores(ids)
	filas, err := r.db.QueryContext(ctx,
		`SELECT id, oferta_id, COALESCE(pack_id, ''), cantidad FROM retenciones WHERE id IN (`+in+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("leer retenciones: %w", err)
	}
	defer filas.Close()
	var res []DatosRetencion
	for filas.Next() {
		var d DatosRetencion
		if err := filas.Scan(&d.ID, &d.OfertaID, &d.PackID, &d.Cantidad); err != nil {
			return nil, err
		}
		res = append(res, d)
	}
	return res, filas.Err()
}

// ConfirmacionGuardada devuelve la respuesta de una compra ya confirmada, o nil.
func (r *Inventario) ConfirmacionGuardada(ctx context.Context, compraID string) (*models.Confirmacion, error) {
	return leerConfirmacion(ctx, r.db, compraID, "")
}

func leerConfirmacion(ctx context.Context, q querier, compraID, bloqueo string) (*models.Confirmacion, error) {
	var crudo []byte
	err := q.QueryRowContext(ctx, `SELECT respuesta FROM confirmaciones WHERE compra_id = ? `+bloqueo, compraID).Scan(&crudo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("leer confirmación: %w", err)
	}
	var c models.Confirmacion
	if err := json.Unmarshal(crudo, &c); err != nil {
		return nil, fmt.Errorf("confirmación guardada ilegible: %w", err)
	}
	return &c, nil
}

var errConfirmacionDuplicada = errors.New("la compra ya fue confirmada por otra solicitud")

// Confirmar pasa a CONFIRMADA todas las retenciones de la compra y suma los
// cupos a vendidos, en una sola transacción: todas o ninguna (RN-02). lineas
// trae, por retención, la descripción y el precio vigente (RN-05, RN-06).
// Es idempotente por compraID: un reintento devuelve la respuesta guardada.
func (r *Inventario) Confirmar(ctx context.Context, compraID, titularID string, ids []string, lineas map[string]models.LineaConfirmada) (models.Confirmacion, error) {
	ids = sinRepetir(ids)
	headers := headersDe(ctx)
	var conf models.Confirmacion
	err := enTransaccion(ctx, r.db, func(tx *sql.Tx) error {
		in, args := marcadores(ids)
		// Lectura sin bloqueo, solo para saber qué inventario bloquear (oferta_id no cambia).
		ofertaIDs, err := columna(ctx, tx, `SELECT DISTINCT oferta_id FROM retenciones WHERE id IN (`+in+`)`, args...)
		if err != nil {
			return err
		}
		if _, err := bloquearInventario(ctx, tx, ofertaIDs); err != nil {
			return err
		}
		// Con el bloqueo tomado: si un reintento concurrente ya confirmó, se devuelve lo mismo.
		if guardada, err := leerConfirmacion(ctx, tx, compraID, "FOR SHARE"); err != nil || guardada != nil {
			if guardada != nil {
				conf = *guardada
			}
			return err
		}

		filas, err := tx.QueryContext(ctx,
			`SELECT id, oferta_id, cantidad, titular_id, estado, expira_en > NOW(6), grupo_id, COALESCE(pack_id, '')
			 FROM retenciones WHERE id IN (`+in+`) FOR UPDATE`, args...)
		if err != nil {
			return fmt.Errorf("bloquear retenciones: %w", err)
		}
		type fila struct {
			ofertaID, titular, estado, grupo, pack string
			cantidad                               int
			vigente                                bool
		}
		encontradas := map[string]fila{}
		for filas.Next() {
			var id string
			var f fila
			if err := filas.Scan(&id, &f.ofertaID, &f.cantidad, &f.titular, &f.estado, &f.vigente, &f.grupo, &f.pack); err != nil {
				filas.Close()
				return err
			}
			encontradas[id] = f
		}
		filas.Close()
		if err := filas.Err(); err != nil {
			return err
		}

		var noVigentes []string
		gruposDePack := map[string]bool{}
		for _, id := range ids {
			f, ok := encontradas[id]
			_, conPrecio := lineas[id]
			if !ok || f.estado != models.RetencionActiva || !f.vigente || f.titular != titularID || !conPrecio {
				noVigentes = append(noVigentes, id)
				continue
			}
			if f.pack != "" {
				gruposDePack[f.grupo] = true
			}
		}
		// Un pack se compra entero: deben venir todas las retenciones activas de su grupo.
		for grupo := range gruposDePack {
			delGrupo, err := columna(ctx, tx, `SELECT id FROM retenciones WHERE grupo_id = ? AND estado = 'ACTIVA'`, grupo)
			if err != nil {
				return err
			}
			if slices.ContainsFunc(delGrupo, func(id string) bool { return !slices.Contains(ids, id) }) {
				for _, id := range ids {
					if encontradas[id].grupo == grupo && !slices.Contains(noVigentes, id) {
						noVigentes = append(noVigentes, id)
					}
				}
			}
		}
		if len(noVigentes) > 0 {
			slices.Sort(noVigentes)
			return &models.ErrRetencionesNoVigentes{IDs: noVigentes}
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE retenciones SET estado = 'CONFIRMADA', compra_id = ? WHERE id IN (`+in+`)`,
			append([]any{compraID}, args...)...); err != nil {
			return fmt.Errorf("confirmar retenciones: %w", err)
		}
		vendidosPorOferta := map[string]int{}
		for _, id := range ids {
			vendidosPorOferta[encontradas[id].ofertaID] += encontradas[id].cantidad
		}
		for _, oferta := range ordenadosSinRepetir(ofertaIDs) {
			if _, err := tx.ExecContext(ctx,
				`UPDATE inventario SET vendidos = vendidos + ?, version = version + 1 WHERE oferta_id = ?`,
				vendidosPorOferta[oferta], oferta); err != nil {
				return fmt.Errorf("sumar vendidos: %w", err)
			}
		}

		conf = models.Confirmacion{CompraID: compraID}
		var total int64
		for _, id := range ids {
			l := lineas[id]
			l.RetencionID = id
			l.OfertaID = encontradas[id].ofertaID
			conf.Lineas = append(conf.Lineas, l)
			total += models.Centavos(l.Precio)
		}
		conf.Total = models.DesdeCentavos(total)
		crudo, err := json.Marshal(conf)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO confirmaciones (compra_id, respuesta) VALUES (?, ?)`, compraID, crudo)
		if esErrorMySQL(err, errClaveDuplicada) {
			return errConfirmacionDuplicada
		}
		if err != nil {
			return fmt.Errorf("guardar confirmación: %w", err)
		}
		for _, oferta := range ordenadosSinRepetir(ofertaIDs) {
			if err := registrarDisponibilidad(ctx, tx, oferta, headers); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errConfirmacionDuplicada) {
		guardada, err := r.ConfirmacionGuardada(ctx, compraID)
		if err != nil || guardada == nil {
			return models.Confirmacion{}, fmt.Errorf("leer confirmación concurrente: %w", err)
		}
		return *guardada, nil
	}
	return conf, err
}

// ---------- Vencer ----------

// VencerPendientes marca VENCIDA las retenciones activas que ya vencieron y
// avisa con RetencionVencida y DisponibilidadCambiada. Una transacción corta
// por oferta, con SKIP LOCKED sobre el inventario: varias instancias pueden
// correrlo a la vez sin pisarse, y nunca hace esperar a un cliente. Si la fila
// está ocupada, la oferta queda para el próximo barrido. La validez de una
// retención no depende de este proceso (RN-11).
func (r *Inventario) VencerPendientes(ctx context.Context, maxOfertas int) (int, error) {
	ofertas, err := columna(ctx, r.db,
		`SELECT DISTINCT oferta_id FROM retenciones WHERE estado = 'ACTIVA' AND expira_en <= NOW(6) LIMIT ?`, maxOfertas)
	if err != nil {
		return 0, err
	}
	headers := headersDe(ctx)
	total := 0
	for _, oferta := range ofertas {
		n := 0
		err := enTransaccion(ctx, r.db, func(tx *sql.Tx) error {
			var id string
			err := tx.QueryRowContext(ctx,
				`SELECT oferta_id FROM inventario WHERE oferta_id = ? FOR UPDATE SKIP LOCKED`, oferta).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				return nil // la tiene otra instancia u otra operación
			}
			if err != nil {
				return err
			}
			filas, err := tx.QueryContext(ctx,
				`SELECT id, titular_id, expira_en FROM retenciones
				 WHERE oferta_id = ? AND estado = 'ACTIVA' AND expira_en <= NOW(6) FOR UPDATE`, oferta)
			if err != nil {
				return err
			}
			var vencidas []eventoRetencionVencida
			for filas.Next() {
				ev := eventoRetencionVencida{Tipo: TipoRetencionVencida, OfertaID: oferta}
				if err := filas.Scan(&ev.RetencionID, &ev.TitularID, &ev.ExpiraEn); err != nil {
					filas.Close()
					return err
				}
				ev.ExpiraEn = ev.ExpiraEn.UTC()
				vencidas = append(vencidas, ev)
			}
			filas.Close()
			if len(vencidas) == 0 {
				return filas.Err()
			}
			ids := make([]string, len(vencidas))
			for i, v := range vencidas {
				ids[i] = v.RetencionID
			}
			in, args := marcadores(ids)
			if _, err := tx.ExecContext(ctx, `UPDATE retenciones SET estado = 'VENCIDA' WHERE id IN (`+in+`)`, args...); err != nil {
				return err
			}
			if err := incrementarVersion(ctx, tx, oferta); err != nil {
				return err
			}
			for _, ev := range vencidas {
				ev.EventID = uuid.NewString()
				if err := insertarOutbox(ctx, tx, TipoRetencionVencida, ev, headers); err != nil {
					return err
				}
			}
			n = len(vencidas)
			return registrarDisponibilidad(ctx, tx, oferta, headers)
		})
		if err != nil {
			return total, fmt.Errorf("vencer retenciones de %s: %w", oferta, err)
		}
		total += n
	}
	return total, nil
}

// ---------- Auxiliares ----------

func incrementarVersion(ctx context.Context, tx *sql.Tx, ofertaID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE inventario SET version = version + 1 WHERE oferta_id = ?`, ofertaID)
	if err != nil {
		return fmt.Errorf("incrementar versión: %w", err)
	}
	return nil
}

// registrarDisponibilidad deja en el outbox el estado completo de la oferta
// (no un delta) con su versión, como pide ADR-015. Se llama con el inventario
// de la oferta ya bloqueado.
func registrarDisponibilidad(ctx context.Context, tx *sql.Tx, ofertaID string, headers string) error {
	var f filaInventario
	if err := tx.QueryRowContext(ctx, `SELECT cupo_total, vendidos, version FROM inventario WHERE oferta_id = ?`, ofertaID).
		Scan(&f.cupoTotal, &f.vendidos, &f.version); err != nil {
		return fmt.Errorf("leer inventario: %w", err)
	}
	retenidos, proxima, err := ocupacion(ctx, tx, ofertaID)
	if err != nil {
		return err
	}
	d := models.CalcularDisponibilidad(ofertaID, f.cupoTotal, f.vendidos, retenidos, proxima, f.version)
	return insertarOutbox(ctx, tx, TipoDisponibilidadCambiada, eventoDisponibilidad{
		EventID: uuid.NewString(), Tipo: TipoDisponibilidadCambiada, OfertaID: ofertaID,
		Estado: d.Estado, Libres: d.Libres, ProximaLiberacion: d.ProximaLiberacion, Version: d.Version,
	}, headers)
}

func columna(ctx context.Context, q querier, consulta string, args ...any) ([]string, error) {
	filas, err := q.QueryContext(ctx, consulta, args...)
	if err != nil {
		return nil, err
	}
	defer filas.Close()
	var res []string
	for filas.Next() {
		var v string
		if err := filas.Scan(&v); err != nil {
			return nil, err
		}
		res = append(res, v)
	}
	return res, filas.Err()
}

func sinRepetir(ids []string) []string {
	vistos := map[string]bool{}
	var res []string
	for _, id := range ids {
		if !vistos[id] {
			vistos[id] = true
			res = append(res, id)
		}
	}
	return res
}

func ordenadosSinRepetir(ids []string) []string {
	res := sinRepetir(ids)
	slices.Sort(res)
	return res
}

func compararTexto(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func nulo(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func truncar(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
