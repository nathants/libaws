package lib

import (
	"fmt"
	"os"
)

func panic1(err error) {
	if err != nil {
		panic(err)
	}
}

func panic2(x interface{}, e error) interface{} {
	if e != nil {
		fmt.Fprintf(os.Stderr, "fatal: %s\n", e)
		os.Exit(1)
	}
	return x
}

func Contains(parts []string, part string) bool {
	for _, p := range parts {
		if p == part {
			return true
		}
	}
	return false
}
