package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr           string
	JevBaseURL     string
	JevAPIKey      string
	JevModel       string
	SerpURL        string
	TopN           int
	RequestTimeout time.Duration
}

func Load() Config {
	return Config{
		Addr:           getenv("SENSE_ADDR", ":2727"),
		JevBaseURL:     getenv("JEV_BASE_URL", "http://localhost:8000"),
		JevAPIKey:      getenv("JEV_API_KEY", ""),
		JevModel:       getenv("JEV_MODEL", ""),
		SerpURL:        getenv("SERP_URL", ""),
		TopN:           getenvInt("TOP_N", 10),
		RequestTimeout: getenvDuration("REQUEST_TIMEOUT", 30*time.Second),
	}
}

func getenv(key, fallback string) string {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}

	return v
}

func getenvInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}

	return n
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}

	return d
}
