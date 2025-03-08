package utils

import (
	"os"
	"strconv"
)

func Config(name string, default_val string) string {
	val := os.Getenv(name)
	if val == "" {
		return default_val
	}
	return val
}

func Configi(name string, default_val int) int {
	val := os.Getenv(name)
	if val == "" {
		return default_val
	}
	ival, err := strconv.Atoi(val)
	if err != nil {
		panic(err)
	}
	return ival
}
