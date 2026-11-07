package main

import (
	"fmt"
	"os"
	"time"

	"agencia/reservas/internal/adapters/mysql"
)

type config struct {
	puertoHTTP          string
	mysql               mysql.Config
	rabbitURL           string
	catalogoURL         string
	estudiarioURL       string
	estudiarioAPIKey    string
	intervaloRecupero   time.Duration
	intervaloEnvio      time.Duration
	intervaloModeracion time.Duration
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
	cfg := config{
		puertoHTTP: conDefecto("PUERTO_HTTP", "8080"),
		mysql: mysql.Config{
			Host:     conDefecto("DB_HOST", "localhost"),
			Puerto:   conDefecto("DB_PUERTO", "3306"),
			Usuario:  conDefecto("DB_USUARIO", "reservas"),
			Password: requerida("DB_PASSWORD"),
			Nombre:   conDefecto("DB_NOMBRE", "reservas"),
		},
		rabbitURL:   requerida("RABBITMQ_URL"),
		catalogoURL: conDefecto("CATALOGO_URL", "http://catalogo:8080"),
		// Estudiario (Grupo 2). Sin URL pública, el mock de su contrato en docker-compose.
		estudiarioURL:    conDefecto("ESTUDIARIO_URL", "http://estudiario-mock:4010"),
		estudiarioAPIKey: os.Getenv("ESTUDIARIO_API_KEY"),
	}
	var err error
	for _, d := range []struct {
		destino *time.Duration
		nombre  string
		defecto string
	}{
		{&cfg.intervaloRecupero, "RECUPERACION_COMPRAS_INTERVALO", "10s"},
		{&cfg.intervaloEnvio, "GUIAS_ENVIO_INTERVALO", "30s"},
		// Estudiario pide no consultar más de una vez por minuto por documento.
		{&cfg.intervaloModeracion, "GUIAS_MODERACION_INTERVALO", "5m"},
	} {
		if *d.destino, err = time.ParseDuration(conDefecto(d.nombre, d.defecto)); err != nil {
			return config{}, fmt.Errorf("%s: %w", d.nombre, err)
		}
	}
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
