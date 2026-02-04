package orm

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func InitORM(host, port, user, password, dbname string) error {
	var err error
	// build the dsn string
	dsn := "host=" + host + " user=" + user + " password=" + password + " dbname=" + dbname + " port=" + port + " sslmode=require TimeZone=UTC"
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	return nil
}

func RegisterModels(models ...interface{}) error {
	return DB.AutoMigrate(models...)
}
