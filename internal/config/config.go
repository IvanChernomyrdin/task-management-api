package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr      string
	MySQLDSN      string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	JWTSecret     string
	JWTIssuer     string
	JWTDuration   time.Duration
}

func Load() (Config, error) {
	redisDB, err := strconv.Atoi(env("REDIS_DB", "0"))
	if err != nil || redisDB < 0 {
		return Config{}, errors.New("invalid REDIS_DB")
	}

	c := Config{
		HTTPAddr:      env("HTTP_ADDR", ":8080"),
		MySQLDSN:      os.Getenv("MYSQL_DSN"),
		RedisAddr:     env("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       redisDB, JWTSecret: os.Getenv("JWT_SECRET"),
		JWTIssuer:   env("JWT_ISSUER", "task-management-api"),
		JWTDuration: 24 * time.Hour,
	}

	if c.MySQLDSN == "" {
		return Config{}, errors.New("MYSQL_DSN must be set")
	}

	if len(c.JWTSecret) < 32 {
		return Config{}, errors.New("JWT_SECRET must contain at least 32 characters")
	}
	return c, nil
}

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
