package common

import "strconv"

func Stoi64(str string) int64 {
	i, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return -1
	}
	return i
}