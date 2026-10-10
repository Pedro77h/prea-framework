package common

import (
	_ "github.com/joho/godotenv/autoload"
	"os"
)

func GetEnv(env string) string {
	return os.Getenv(env)
}