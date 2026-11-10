package mysql

import "time"

type usuarioModelo struct {
	ID           string    `gorm:"type:char(36);primaryKey"`
	Nombre       string    `gorm:"size:100;not null"`
	Email        string    `gorm:"size:254;not null;uniqueIndex"`
	Rol          string    `gorm:"size:16;not null"`
	HashPassword string    `gorm:"size:60;not null"`
	CreadoEn     time.Time `gorm:"not null"`
}

func (usuarioModelo) TableName() string { return "usuarios" }

type appLogModelo struct {
	ID            uint64    `gorm:"primaryKey;autoIncrement"`
	Fecha         time.Time `gorm:"type:datetime(6);not null;index:idx_app_logs_fecha"`
	Nivel         string    `gorm:"size:16;not null"`
	Servicio      string    `gorm:"size:100;not null"`
	Mensaje       string    `gorm:"type:text;not null"`
	CorrelacionID *string   `gorm:"size:64;index:idx_app_logs_correlacion"`
	TraceID       *string   `gorm:"type:char(32);index:idx_app_logs_trace"`
	Metadatos     string    `gorm:"type:json;not null"`
}

func (appLogModelo) TableName() string { return "app_logs" }
