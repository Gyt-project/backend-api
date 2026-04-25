package orm

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitORM(host, port, user, password, dbname string) error {
	var err error
	// build the dsn string
	dsn := "host=" + host + " user=" + user + " password=" + password + " dbname=" + dbname + " port=" + port + " sslmode=require TimeZone=UTC"

	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.New(
			gormStdLogger{},
			logger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  logger.Warn,
				IgnoreRecordNotFoundError: true,
				Colorful:                  true,
			},
		),
	})
	if err != nil {
		return err
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	return nil
}

func RegisterModels(models ...interface{}) error {
	return DB.AutoMigrate(models...)
}

// gormStdLogger wraps the standard log output so GORM's logger.New accepts it.
type gormStdLogger struct{}

func (gormStdLogger) Printf(format string, args ...interface{}) {
	// delegate to standard log
	_ = format
	fmt.Printf(format, args...)
}
