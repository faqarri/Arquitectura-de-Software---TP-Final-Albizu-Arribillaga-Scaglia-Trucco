package main

import (
	"fmt"
	"os"

	"agencia/usuarios/internal/adapters/mysql"
)

type config struct {
	puertoHTTP string
	mysql      mysql.Config
	secretoJWT string
	adminEmail string
	adminPass  string
}

func cargarConfig() (config, error) {
	cfg := config{
		puertoHTTP: conDefecto("PUERTO_HTTP", "8080"),
		mysql: mysql.Config{
			Host:     conDefecto("DB_HOST", "localhost"),
			Puerto:   conDefecto("DB_PUERTO", "3306"),
			Usuario:  conDefecto("DB_USUARIO", "usuarios"),
			Password: os.Getenv("DB_PASSWORD"),
			Nombre:   conDefecto("DB_NOMBRE", "usuarios"),
		},
		secretoJWT: os.Getenv("JWT_SECRET"),
		adminEmail: os.Getenv("USUARIOS_ADMIN_EMAIL"),
		adminPass:  os.Getenv("USUARIOS_ADMIN_PASSWORD"),
	}
	var faltantes []string
	if cfg.mysql.Password == "" {
		faltantes = append(faltantes, "DB_PASSWORD")
	}
	if len(cfg.secretoJWT) < 32 {
		faltantes = append(faltantes, "JWT_SECRET (mínimo 32 caracteres)")
	}
	if (cfg.adminEmail == "") != (cfg.adminPass == "") {
		return config{}, fmt.Errorf("USUARIOS_ADMIN_EMAIL y USUARIOS_ADMIN_PASSWORD deben definirse juntas")
	}
	if len(faltantes) != 0 {
		return config{}, fmt.Errorf("faltan variables de entorno válidas: %v", faltantes)
	}
	return cfg, nil
}

func conDefecto(nombre, defecto string) string {
	if valor := os.Getenv(nombre); valor != "" {
		return valor
	}
	return defecto
}
