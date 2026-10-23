package main

import (
	"fmt"
	"os"

	"agencia/comprobantes/internal/adapters/api"
	"agencia/comprobantes/internal/adapters/mysql"
	"agencia/comprobantes/internal/adapters/rabbit"
	"agencia/comprobantes/internal/adapters/smtp"
)

type config struct {
	puertoHTTP string
	mysql      mysql.Config
	rabbit     rabbit.Config
	smtp       smtp.Config
	apiKeys    map[string]string
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
			Usuario:  conDefecto("DB_USUARIO", "comprobantes"),
			Password: requerida("DB_PASSWORD"),
			Nombre:   conDefecto("DB_NOMBRE", "comprobantes"),
		},
		rabbit: rabbit.Config{URL: requerida("RABBITMQ_URL")},
		smtp: smtp.Config{
			Host:      conDefecto("SMTP_HOST", "localhost"),
			Puerto:    conDefecto("SMTP_PUERTO", "1025"),
			Remitente: conDefecto("SMTP_REMITENTE", "comprobantes@agencia-viajes.example"),
			Usuario:   os.Getenv("SMTP_USUARIO"),
			Password:  os.Getenv("SMTP_PASSWORD"),
		},
	}
	claves := requerida("COMPROBANTES_API_KEYS")

	if len(faltantes) > 0 {
		return config{}, fmt.Errorf("faltan variables de entorno: %v", faltantes)
	}
	var err error
	if cfg.apiKeys, err = api.ParsearAPIKeys(claves); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func conDefecto(nombre, defecto string) string {
	if v := os.Getenv(nombre); v != "" {
		return v
	}
	return defecto
}
