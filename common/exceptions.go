package common

import "log"

func ThrowException(err error, logger *log.Logger) {
	if err != nil {
		logger.Fatal(err)
	}
}