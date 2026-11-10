package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"agencia/catalogo/internal/models"
)

// ConectarMongo abre el cliente y espera hasta 2 minutos a que MongoDB responda.
func ConectarMongo(ctx context.Context, uri, base string, log *slog.Logger) (*mongo.Database, error) {
	cli, err := mongo.Connect(options.Client().ApplyURI(uri).
		SetServerSelectionTimeout(3 * time.Second).
		SetTimeout(5 * time.Second))
	if err != nil {
		return nil, fmt.Errorf("configurar MongoDB: %w", err)
	}
	limite := time.Now().Add(2 * time.Minute)
	espera := time.Second
	for {
		ping, cancelar := context.WithTimeout(ctx, 3*time.Second)
		err = cli.Ping(ping, nil)
		cancelar()
		if err == nil {
			return cli.Database(base), nil
		}
		if time.Now().After(limite) || ctx.Err() != nil {
			_ = cli.Disconnect(context.Background())
			return nil, fmt.Errorf("conectar a MongoDB: %w", err)
		}
		log.Warn("MongoDB todavía no responde, se reintenta", "error", err, "espera", espera.String())
		select {
		case <-ctx.Done():
		case <-time.After(espera):
		}
		espera = min(espera*2, 10*time.Second)
	}
}

// MigrarMongo crea los índices. El parcial sobre pendientePublicar hace barata
// la consulta del publicador de OfertaActualizada, que corre cada segundo.
func MigrarMongo(ctx context.Context, db *mongo.Database) error {
	_, err := db.Collection("ofertas").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "pendientePublicar", Value: 1}},
			Options: options.Index().SetPartialFilterExpression(bson.D{{Key: "pendientePublicar", Value: true}}),
		},
		{Keys: bson.D{{Key: "destino", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("crear índices de ofertas: %w", err)
	}
	return nil
}

// docOferta es el documento de una oferta. version y pendientePublicar son el
// "outbox dentro del documento" de ADR-006: cada escritura los actualiza en la
// misma operación atómica que los datos.
type docOferta struct {
	ID                string    `bson:"_id"`
	Tipo              string    `bson:"tipo"`
	SubtipoTransporte string    `bson:"subtipoTransporte,omitempty"`
	Titulo            string    `bson:"titulo"`
	Descripcion       string    `bson:"descripcion"`
	Destino           string    `bson:"destino"`
	Origen            string    `bson:"origen,omitempty"`
	Desde             time.Time `bson:"desde"`
	Hasta             time.Time `bson:"hasta"`
	Precio            float64   `bson:"precio"`
	Publicada         bool      `bson:"publicada"`
	Version           int64     `bson:"version"`
	CreadaEn          time.Time `bson:"creadaEn"`
	ActualizadaEn     time.Time `bson:"actualizadaEn"`
	PendientePublicar bool      `bson:"pendientePublicar"`
}

func (d docOferta) modelo() models.Oferta {
	return models.Oferta{
		ID: d.ID, Tipo: models.TipoOferta(d.Tipo), SubtipoTransporte: models.SubtipoTransporte(d.SubtipoTransporte),
		Titulo: d.Titulo, Descripcion: d.Descripcion, Destino: d.Destino, Origen: d.Origen,
		Desde: d.Desde.UTC(), Hasta: d.Hasta.UTC(), Precio: d.Precio, Publicada: d.Publicada, Version: d.Version,
	}
}

func camposEditables(o models.Oferta) bson.D {
	return bson.D{
		{Key: "tipo", Value: string(o.Tipo)},
		{Key: "subtipoTransporte", Value: string(o.SubtipoTransporte)},
		{Key: "titulo", Value: o.Titulo},
		{Key: "descripcion", Value: o.Descripcion},
		{Key: "destino", Value: o.Destino},
		{Key: "origen", Value: o.Origen},
		{Key: "desde", Value: o.Desde},
		{Key: "hasta", Value: o.Hasta},
		{Key: "precio", Value: o.Precio},
	}
}

// Ofertas guarda las descripciones y precios de las ofertas (fuente de verdad).
type Ofertas struct {
	col *mongo.Collection
}

func NuevoOfertas(db *mongo.Database) *Ofertas { return &Ofertas{col: db.Collection("ofertas")} }

// Crear guarda una oferta nueva, ya publicada y pendiente de avisar.
func (r *Ofertas) Crear(ctx context.Context, o models.Oferta) (models.Oferta, error) {
	ahora := time.Now().UTC()
	d := docOferta{
		ID: o.ID, Tipo: string(o.Tipo), SubtipoTransporte: string(o.SubtipoTransporte), Titulo: o.Titulo,
		Descripcion: o.Descripcion, Destino: o.Destino, Origen: o.Origen, Desde: o.Desde, Hasta: o.Hasta,
		Precio: o.Precio, Publicada: true, Version: 1, CreadaEn: ahora, ActualizadaEn: ahora, PendientePublicar: true,
	}
	if _, err := r.col.InsertOne(ctx, d); err != nil {
		return models.Oferta{}, fmt.Errorf("guardar oferta: %w", err)
	}
	return d.modelo(), nil
}

// Obtener devuelve la oferta, publicada o no.
func (r *Ofertas) Obtener(ctx context.Context, id string) (models.Oferta, error) {
	var d docOferta
	err := r.col.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return models.Oferta{}, models.ErrNoEncontrado
	}
	if err != nil {
		return models.Oferta{}, fmt.Errorf("leer oferta: %w", err)
	}
	return d.modelo(), nil
}

// ObtenerVarias devuelve las ofertas encontradas, indexadas por id.
func (r *Ofertas) ObtenerVarias(ctx context.Context, ids []string) (map[string]models.Oferta, error) {
	res := map[string]models.Oferta{}
	if len(ids) == 0 {
		return res, nil
	}
	cur, err := r.col.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}})
	if err != nil {
		return nil, fmt.Errorf("leer ofertas: %w", err)
	}
	var docs []docOferta
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("leer ofertas: %w", err)
	}
	for _, d := range docs {
		res[d.ID] = d.modelo()
	}
	return res, nil
}

// Actualizar reemplaza los datos editables. En la misma escritura atómica sube
// la versión y marca la oferta para publicar OfertaActualizada (ADR-006).
func (r *Ofertas) Actualizar(ctx context.Context, o models.Oferta) (models.Oferta, error) {
	set := append(camposEditables(o),
		bson.E{Key: "actualizadaEn", Value: time.Now().UTC()},
		bson.E{Key: "pendientePublicar", Value: true},
		bson.E{Key: "publicada", Value: true})
	return r.modificar(ctx, o.ID, set)
}

// Despublicar es la baja lógica: la oferta deja de verse pero no se borra, así
// la baja sigue la misma regla de versiones que cualquier cambio (ADR-006).
func (r *Ofertas) Despublicar(ctx context.Context, id string) (models.Oferta, error) {
	return r.modificar(ctx, id, bson.D{
		{Key: "publicada", Value: false},
		{Key: "actualizadaEn", Value: time.Now().UTC()},
		{Key: "pendientePublicar", Value: true},
	})
}

func (r *Ofertas) modificar(ctx context.Context, id string, set bson.D) (models.Oferta, error) {
	var d docOferta
	err := r.col.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: id}},
		bson.D{{Key: "$set", Value: set}, {Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return models.Oferta{}, models.ErrNoEncontrado
	}
	if err != nil {
		return models.Oferta{}, fmt.Errorf("modificar oferta: %w", err)
	}
	return d.modelo(), nil
}

func (r *Ofertas) PendientesDePublicar(ctx context.Context, limite int) ([]models.CambioPendiente, error) {
	cur, err := r.col.Find(ctx, bson.D{{Key: "pendientePublicar", Value: true}},
		options.Find().SetLimit(int64(limite)).SetProjection(bson.D{{Key: "_id", Value: 1}, {Key: "version", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("leer ofertas pendientes de publicar: %w", err)
	}
	var docs []struct {
		ID      string `bson:"_id"`
		Version int64  `bson:"version"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	res := make([]models.CambioPendiente, len(docs))
	for i, d := range docs {
		res[i] = models.CambioPendiente{OfertaID: d.ID, Version: d.Version}
	}
	return res, nil
}

// MarcarPublicada desmarca la oferta solo si la versión no cambió. Si alguien
// la modificó mientras se publicaba, la marca sigue y se publica de nuevo.
func (r *Ofertas) MarcarPublicada(ctx context.Context, c models.CambioPendiente) error {
	_, err := r.col.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: c.OfertaID}, {Key: "version", Value: c.Version}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "pendientePublicar", Value: false}}}})
	return err
}

// ---------- Packs ----------

type docPack struct {
	ID                  string    `bson:"_id"`
	Titulo              string    `bson:"titulo"`
	Destino             string    `bson:"destino"`
	Descripcion         string    `bson:"descripcion,omitempty"`
	DescuentoPorcentaje float64   `bson:"descuentoPorcentaje"`
	Opciones            []string  `bson:"opciones"`
	CreadoEn            time.Time `bson:"creadoEn"`
}

func (d docPack) modelo() models.Pack {
	return models.Pack{ID: d.ID, Titulo: d.Titulo, Destino: d.Destino, Descripcion: d.Descripcion,
		DescuentoPorcentaje: d.DescuentoPorcentaje, OpcionIDs: d.Opciones}
}

type Packs struct {
	col *mongo.Collection
}

func NuevoPacks(db *mongo.Database) *Packs { return &Packs{col: db.Collection("packs")} }

func (r *Packs) Crear(ctx context.Context, p models.Pack) error {
	_, err := r.col.InsertOne(ctx, docPack{
		ID: p.ID, Titulo: p.Titulo, Destino: p.Destino, Descripcion: p.Descripcion,
		DescuentoPorcentaje: p.DescuentoPorcentaje, Opciones: p.OpcionIDs, CreadoEn: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("guardar pack: %w", err)
	}
	return nil
}

func (r *Packs) Obtener(ctx context.Context, id string) (models.Pack, error) {
	var d docPack
	err := r.col.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return models.Pack{}, models.ErrNoEncontrado
	}
	if err != nil {
		return models.Pack{}, fmt.Errorf("leer pack: %w", err)
	}
	return d.modelo(), nil
}

// Listar devuelve los packs más nuevos primero.
func (r *Packs) Listar(ctx context.Context, limite int) ([]models.Pack, error) {
	cur, err := r.col.Find(ctx, bson.D{}, options.Find().SetSort(bson.D{{Key: "creadoEn", Value: -1}}).SetLimit(int64(limite)))
	if err != nil {
		return nil, fmt.Errorf("listar packs: %w", err)
	}
	var docs []docPack
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	res := make([]models.Pack, len(docs))
	for i, d := range docs {
		res[i] = d.modelo()
	}
	return res, nil
}
