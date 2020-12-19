package lib

import (
	"fmt"
	"github.com/avast/retry-go"
	"log"
	"os"
	"reflect"
	"runtime"
	"time"
)

var Logger = log.New(os.Stderr, "", log.Llongfile) // log.Ldate|log.Ltime)

func functionName(i interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
}

func Retry(fn func() error) error {
	count := 0
	attempts := 6
	return retry.Do(
		func() error {
			if count != 0 {
				Logger.Printf("retry %d/%d for %v\n", count, attempts-1, functionName(fn))
			}
			count++
			err := fn()
			if err != nil {
				return err
			}
			return nil
		},
		retry.LastErrorOnly(true),
		retry.Attempts(uint(attempts)),
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
