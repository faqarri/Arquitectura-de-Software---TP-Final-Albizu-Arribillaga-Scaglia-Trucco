package gateway

import (
	"errors"
	"fmt"
	"net/url"
)

type Config struct {
	SecretoJWT           string
	UsuariosURL          string
	ReservasURL          string
	CatalogoURLs         []string
	TiempoRealURL        string
	LimiteSolicitudesMin int
}

func (c Config) Validar() error {
	if len(c.SecretoJWT) < 32 {
		return errors.New("JWT_SECRET debe tener al menos 32 caracteres")
	}
	for nombre, direccion := range map[string]string{
		"USUARIOS_URL": c.UsuariosURL, "RESERVAS_URL": c.ReservasURL,
	} {
		if err := validarURL(direccion); err != nil {
			return fmt.Errorf("%s: %w", nombre, err)
		}
	}
	for i, direccion := range c.CatalogoURLs {
		if err := validarURL(direccion); err != nil {
			return fmt.Errorf("CATALOGO_INSTANCIAS[%d]: %w", i, err)
		}
	}
	if c.TiempoRealURL != "" {
		if err := validarURL(c.TiempoRealURL); err != nil {
			return fmt.Errorf("TIEMPO_REAL_URL: %w", err)
		}
	}
	if c.LimiteSolicitudesMin < 1 {
		return errors.New("GATEWAY_RATE_LIMIT_POR_MINUTO debe ser mayor que cero")
	}
	return nil
}

func validarURL(direccion string) error {
	u, err := url.Parse(direccion)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("debe ser una URL http(s) válida sin credenciales")
	}
	return nil
}
