package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServerPort       string
	JWTAccessSecret  string
	JWTRefreshSecret string
	AccessTime       time.Duration
	RefreshTime      time.Duration
	DBDSN            string
}

func LoadConfig() *Config {
	return &Config{ // все горутины будут работать с одним конфигом по указателю, поэтому можно его оперативно менять ну и плюс не будет возникать постоянных копий
		ServerPort:       getEnvString("SERVER_PORT", ":8000"),
		JWTAccessSecret:  getEnvString("JWT_ACCESS_SECRET", "F8p0OkFJvXSbfh8nVyP8hzcbmVhwfL6C7fQx6tcENBN"),
		JWTRefreshSecret: getEnvString("JWT_REFRESH_SECRET", "qWgbTHLYOmi8gtipk2ESGdcYdb2BMI3XV0k3KGfZAFW"),
		AccessTime:       getEnvDuration("ACCESS_TIME_MINUTE", 15) * time.Minute,
		RefreshTime:      getEnvDuration("REFRESH_TIME_HOURS", 7*24) * time.Hour,
		DBDSN:            getEnvString("DB_DSN", "postgres://postgres:1423qewr@localhost:5432/accelerator"),
	}

}

func getEnvString(key, default_value string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return default_value
}

func getEnvDuration(key string, default_duration int) time.Duration {
	if value := os.Getenv(key); value != "" {
		value_int, err := strconv.Atoi(value)
		if err != nil {
			panic("Недопустимое время жизни токена в .env")
		}
		return time.Duration(value_int)
	}

	return time.Duration(default_duration)
}
