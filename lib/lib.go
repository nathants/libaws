package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/avast/retry-go"
)

type LogWriter struct{}

func (LogWriter) Write(p []byte) (int, error) {
	sep1 := []byte(".go:")
	sep2 := []byte("/")
	parts := bytes.SplitN(p, sep1, 2)
	filepath := bytes.Split(parts[0], sep2)
	keep := [][]byte{
		filepath[len(filepath)-2],
		filepath[len(filepath)-1],
	}
	parts[0] = bytes.Join(keep, sep2)
	return os.Stderr.Write(bytes.Join(parts, sep1))
}

var Logger = log.New(LogWriter{}, "", log.Llongfile)

func SignalHandler(cancel func()) {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		Logger.Println("signal handler")
		cancel()
	}()
}

func functionName(i interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
}

func DropLinesWithAny(s string, tokens ...string) string {
	var lines []string
outer:
	for _, line := range strings.Split(s, "\n") {
		for _, token := range tokens {
			if strings.Contains(line, token) {
				continue outer
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func Pformat(i interface{}) string {
	val, err := json.MarshalIndent(i, "", "    ")
	if err != nil {
		panic(err)
	}
	return string(val)
}

func Retry(ctx context.Context, fn func() error) error {
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
		retry.Context(ctx),
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

func Chunk(xs []string, chunkSize int) [][]string {
	var xss [][]string
	xss = append(xss, []string{})
	for _, x := range xs {
		xss[len(xss)-1] = append(xss[len(xss)-1], x)
		if len(xss[len(xss)-1]) == chunkSize {
			xss = append(xss, []string{})
		}
	}
	return xss
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
