package mysql

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"gorm.io/gorm"

	"agencia/usuarios/internal/domain"
)

var db *gorm.DB

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	contenedor, err := tcmysql.Run(ctx, "mysql:8.4",
		tcmysql.WithDatabase("usuarios"), tcmysql.WithUsername("usuarios"), tcmysql.WithPassword("prueba"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "no se pudo levantar MySQL (¿Docker está corriendo?):", err)
		os.Exit(1)
	}
	codigo := func() int {
		host, err := contenedor.Host(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		puerto, err := contenedor.MappedPort(ctx, "3306/tcp")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		db, err = Conectar(ctx, Config{
			Host: host, Puerto: puerto.Port(), Usuario: "usuarios", Password: "prueba", Nombre: "usuarios",
		}, log)
		if err == nil {
			err = Migrar(db)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return m.Run()
	}()
	if err := contenedor.Terminate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "no se pudo detener el contenedor MySQL:", err)
		if codigo == 0 {
			codigo = 1
		}
	}
	os.Exit(codigo)
}

func TestRepositorioCrearBuscarYRechazarEmailDuplicado(t *testing.T) {
	repo := NuevoRepositorio(db)
	usuario := &domain.Usuario{
		ID: "e081f5c2-3b3a-43a8-88b6-9ef63a29a3d8", Nombre: "Ana Pérez", Email: "ana@example.com",
		Rol: domain.RolCliente, HashPassword: "$2a$10$abcdefghijklmnopqrstuu9CxQw7Uv2Mn4YBxPCNVx9xVku3lUu",
		CreadoEn: time.Now().UTC(),
	}
	ctx := context.Background()
	if err := repo.Crear(ctx, usuario); err != nil {
		t.Fatal(err)
	}
	guardado, err := repo.PorEmail(ctx, usuario.Email)
	if err != nil {
		t.Fatal(err)
	}
	if guardado.ID != usuario.ID || guardado.Email != usuario.Email || guardado.HashPassword != usuario.HashPassword {
		t.Fatalf("usuario leído distinto del guardado: %+v", guardado)
	}
	if err := repo.Crear(ctx, usuario); err != domain.ErrEmailEnUso {
		t.Fatalf("se esperaba email duplicado, got %v", err)
	}
}

func TestRepositorioEmailInexistente(t *testing.T) {
	_, err := NuevoRepositorio(db).PorEmail(context.Background(), "inexistente@example.com")
	if err != domain.ErrNoEncontrado {
		t.Fatalf("se esperaba usuario inexistente, got %v", err)
	}
}
