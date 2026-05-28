package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerPort string
	ClientURLs []string

	JWTAccessSecret  string
	JWTRefreshSecret string
	AccessTime       time.Duration
	RefreshTime      time.Duration

	MinioEndpoint           string
	MinioAccessKey          string
	MinioSecretKey          string
	MinioBucket             string
	MinioSSL                bool
	PresignedPublicHostName string

	DBDSN string

	SizeLimitAudioMB        int
	GeneratePasswordLength  int
	LimitUploadAudioMinutes time.Duration
	MaxUploadWorkers        int
	AIWorkersTimeoutHour    time.Duration
	LimitAudioURLMinuts     time.Duration

	TotalVRAMGB int
	TotalRAMGB  int
}

func LoadConfig() *Config {
	return &Config{ // все горутины будут работать с одним конфигом по указателю, поэтому можно его оперативно менять ну и плюс не будет возникать постоянных копий
		ServerPort: getEnvString("SERVER_PORT", ":8000"),
		ClientURLs: getEnvStringSlice("CLIENT_URLS", []string{
			"http://localhost:5173", // базовые адреса для VITE
			"http://127.0.0.1:5173",
			"http://localhost:4173",
			"http://127.0.0.1:4173",
		}),

		JWTAccessSecret:  getEnvString("JWT_ACCESS_SECRET", "F8p0OkFJvXSbfh8nVyP8hzcbmVhwfL6C7fQx6tcENBN"),
		JWTRefreshSecret: getEnvString("JWT_REFRESH_SECRET", "qWgbTHLYOmi8gtipk2ESGdcYdb2BMI3XV0k3KGfZAFW"),
		AccessTime:       getEnvDuration("ACCESS_TIME_MINUTE", 15) * time.Minute,
		RefreshTime:      getEnvDuration("REFRESH_TIME_HOURS", 7*24) * time.Hour,

		MinioEndpoint:  getEnvString("S3_ENDPOINT", "minio:9000"),
		MinioAccessKey: getEnvString("S3_ACCESS_KEY_ID", "minioadmin"),
		MinioSecretKey: getEnvString("S3_SECRET_ACCESS_KEY", "minioadmin"),
		MinioBucket:    getEnvString("S3_BUCKET", "neurodoc"),
		MinioSSL:       getEnvBool("S3_USESSL", false),
		PresignedPublicHostName: getEnvString("PRESIGNED_PUBLIC_HOST_NAME", "localhost:9000"),

		DBDSN:                   getEnvString("DB_DSN", "postgres://nikita:1423qewr@postgres:5432/accelerator"),
		SizeLimitAudioMB:        getEnvInt("SIZE_LIMIT_AUDIO_MB", 1024),
		GeneratePasswordLength:  getEnvInt("GENERATE_PASSWORD_LENGTH", 10),
		LimitUploadAudioMinutes: getEnvDuration("LIMIT_UPLOAD_AUDIO_MINUTE", 30) * time.Minute,
		MaxUploadWorkers:        getEnvInt("MAX_UPLOAD_WORKERS", 20),
		AIWorkersTimeoutHour:    getEnvDuration("AI_WORKERS_HTTP_TIMEOUT_HOURS", 2) * time.Hour,
		LimitAudioURLMinuts:     getEnvDuration("LIMIT_AUDIO_URL_MINUTS", 60) * time.Minute,

		TotalVRAMGB: getEnvInt("TOTAL_VRAM_GB", 12),
		TotalRAMGB:  getEnvInt("TOTAL_RAM_GB", 16),
	}

}

func getEnvString(key, default_value string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return default_value
}

func getEnvInt(key string, default_value int) int {
	if value := os.Getenv(key); value != "" {
		valueInt, err := strconv.Atoi(value)
		if err != nil {
			panic("Недопустимый максимальный размер аудио в .env")
		}
		return valueInt
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

func getEnvBool(key string, default_value bool) bool {
	if value := os.Getenv(key); value != "" {
		switch value {
		case "true":
			return true
		case "false":
			return false
		default:
			panic("Недопустимое значение ssl mode: true/false")
		}
	}

	return default_value
}

func getEnvStringSlice(key string, default_value []string) []string {
	if value := os.Getenv(key); value != "" {
		valueSlice := strings.Split(value, ",")

		cleaned := make([]string, 0, len(valueSlice))

		for _, part := range valueSlice {
			// Удаляем лишние пробелы по краям
			trimmed := strings.TrimSpace(part)

			cleaned = append(cleaned, trimmed)
		}

		return cleaned

	}

	return default_value
}
