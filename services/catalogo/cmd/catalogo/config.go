package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"agencia/catalogo/internal/repository"
)

type config struct {
	puertoHTTP       string
	instancia        string
	mysql            repository.ConfigMySQL
	mongoURL         string
	mongoBase        string
	rabbitURL        string
	redisURL         string
	cacheHabilitada  bool
	estudiarioURL    string
	estudiarioAPIKey string
	opensearchURL    string
	aliasBusqueda    string
	retencionMinutos int
	maxPorOferta     int
	intervaloBarrido time.Duration
}

// cargarConfig lee la configuración de variables de entorno. Los secretos no
// tienen valor por defecto: si faltan, el servicio no arranca.
func cargarConfig() (config, error) {
	var faltantes []string
	requerida := func(nombre string) string {
		v := os.Getenv(nombre)
		if v == "" {
			faltantes = append(faltantes, nombre)
		}
		return v
	}
	host, _ := os.Hostname()
	cfg := config{
		puertoHTTP: conDefecto("PUERTO_HTTP", "8080"),
		instancia:  conDefecto("INSTANCIA_ID", host),
		mysql: repository.ConfigMySQL{
			Host:     conDefecto("DB_HOST", "localhost"),
			Puerto:   conDefecto("DB_PUERTO", "3306"),
			Usuario:  conDefecto("DB_USUARIO", "catalogo"),
			Password: requerida("DB_PASSWORD"),
			Nombre:   conDefecto("DB_NOMBRE", "catalogo"),
		},
		mongoURL:         conDefecto("MONGO_URL", "mongodb://localhost:27017"),
		mongoBase:        conDefecto("MONGO_BASE", "catalogo"),
		rabbitURL:        requerida("RABBITMQ_URL"),
		redisURL:         os.Getenv("REDIS_URL"),
		cacheHabilitada:  conDefecto("CACHE_HABILITADA", "true") == "true",
		estudiarioURL:    conDefecto("ESTUDIARIO_URL", "http://estudiario-mock:4010"),
		estudiarioAPIKey: os.Getenv("ESTUDIARIO_API_KEY"),
		opensearchURL:    conDefecto("OPENSEARCH_URL", "http://localhost:9200"),
		aliasBusqueda:    conDefecto("BUSQUEDA_ALIAS", "ofertas"),
	}
	var err error
	if cfg.retencionMinutos, err = entero("RETENCION_MINUTOS", 15); err != nil {
		return config{}, err
	}
	if cfg.maxPorOferta, err = entero("RETENCION_MAX_CUPOS_POR_OFERTA", 6); err != nil {
		return config{}, err
	}
	segundos, err := entero("RETENCION_BARRIDO_SEGUNDOS", 15)
	if err != nil {
		return config{}, err
	}
	cfg.intervaloBarrido = time.Duration(segundos) * time.Second
	if len(faltantes) > 0 {
		return config{}, fmt.Errorf("faltan variables de entorno: %v", faltantes)
	}
	return cfg, nil
}

func conDefecto(nombre, defecto string) string {
	if v := os.Getenv(nombre); v != "" {
		return v
	}
	return defecto
}

func entero(nombre string, defecto int) (int, error) {
	v := os.Getenv(nombre)
	if v == "" {
		return defecto, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s debe ser un entero positivo (vale %q)", nombre, v)
	}
	return n, nil
}
