package lib

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

type logger struct {
	disabled bool
}

var Logger = &logger{
	disabled: strings.ToLower(os.Getenv("LOGGING") + " ")[:1] == "n",
}

func caller() string {
	_, file, line, _ := runtime.Caller(2)
	parts := strings.Split(file, "/")
	keep := []string{
		parts[len(parts)-2],
		parts[len(parts)-1],
	}
	file = strings.Join(keep, "/")
	return fmt.Sprintf("%s:%d:", file, line)
}

func (l *logger) Println(v ...interface{}) {
	if !l.disabled {
		var r []interface{}
		r = append(r, caller())
		r = append(r, v...)
		fmt.Fprintln(os.Stderr, r...)
	}
}

func (l *logger) Printf(format string, v ...interface{}) {
	if !l.disabled {
		fmt.Fprintf(os.Stderr, caller()+" "+format, v...)
	}
}

func (l *logger) Fatal(v ...interface{}) {
	var r []interface{}
	r = append(r, caller())
	r = append(r, v...)
	fmt.Fprintln(os.Stderr, r...)
	os.Exit(1)
}

func (l *logger) Fatalf(format string, v ...interface{}) {
	fmt.Fprintf(os.Stderr, caller()+" "+format, v...)
	os.Exit(1)
}
