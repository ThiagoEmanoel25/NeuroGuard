package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config centraliza todas as configurações do gateway.
// Carregadas de variáveis de ambiente — nunca hardcode em produção.
type Config struct {
	Server   ServerConfig
	JWT      JWTConfig
	Redis    RedisConfig
	Services ServicesConfig
}

type ServerConfig struct {
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	Env          string // "development" | "production"
}

type JWTConfig struct {
	Secret          string
	ExpiryHours     int
	RefreshExpiry   time.Duration
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// ServicesConfig guarda os endereços internos dos microsserviços.
// Em Kubernetes, esses são os nomes dos Services do cluster.
type ServicesConfig struct {
	CrisisServiceURL    string // grpc://crisis-service:50051
	TelemetryServiceURL string // grpc://telemetry-service:50052
	ClinicalServiceURL  string // grpc://clinical-service:50053
	MLServiceURL        string // grpc://ml-inference:50054
}

// Load lê variáveis de ambiente e devolve Config preenchido.
// Falha explicitamente se variáveis críticas estiverem ausentes.
func Load() (*Config, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET não definido — defina a variável de ambiente")
	}

	jwtExpiry, err := strconv.Atoi(getEnvOrDefault("JWT_EXPIRY_HOURS", "24"))
	if err != nil {
		return nil, fmt.Errorf("JWT_EXPIRY_HOURS inválido: %w", err)
	}

	redisDB, err := strconv.Atoi(getEnvOrDefault("REDIS_DB", "0"))
	if err != nil {
		return nil, fmt.Errorf("REDIS_DB inválido: %w", err)
	}

	return &Config{
		Server: ServerConfig{
			Port:         getEnvOrDefault("PORT", "8080"),
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			Env:          getEnvOrDefault("ENV", "development"),
		},
		JWT: JWTConfig{
			Secret:        jwtSecret,
			ExpiryHours:   jwtExpiry,
			RefreshExpiry: 7 * 24 * time.Hour,
		},
		Redis: RedisConfig{
			Addr:     getEnvOrDefault("REDIS_ADDR", "localhost:6379"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       redisDB,
		},
		Services: ServicesConfig{
			CrisisServiceURL:    getEnvOrDefault("CRISIS_SERVICE_URL", "localhost:50051"),
			TelemetryServiceURL: getEnvOrDefault("TELEMETRY_SERVICE_URL", "localhost:50052"),
			ClinicalServiceURL:  getEnvOrDefault("CLINICAL_SERVICE_URL", "localhost:50053"),
			MLServiceURL:        getEnvOrDefault("ML_SERVICE_URL", "localhost:50054"),
		},
	}, nil
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}