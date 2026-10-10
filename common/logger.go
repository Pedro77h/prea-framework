package common

import (
	"log"
)

type ILogger interface {
	GetLogger() *log.Logger
}

func MakeLogger(name string) *log.Logger {
	logger := log.Default()
	logger.SetPrefix(name + ": ")

	return logger
}