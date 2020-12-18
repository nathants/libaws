package lib

import (
	"fmt"
	"time"
	"os"
	"github.com/avast/retry-go"
	"log"
)

var Logger = log.New(os.Stderr, "", log.Lshortfile) // log.Ldate|log.Ltime)

func Retry(fn func() error) error {
	return retry.Do(
		fn,
		retry.LastErrorOnly(true),
		retry.Attempts(6),
		retry.Delay(150*time.Millisecond),
	)
}

func Assert(cond bool, format string, a ...interface{}) {
	if !cond {
		panic(fmt.Sprintf(format, a...))
	}
}

func Panic1(err error) {
	if err != nil {
		panic(err)
	}
}

func Panic2(x interface{}, e error) interface{} {
	if e != nil {
		Logger.Printf("fatal: %s\n", e)
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
