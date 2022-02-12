package lib

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

type LoggerStruct struct {
	PrintFn  func(args ...interface{})
	FlushFn  func()
	disabled bool
}

var Logger = &LoggerStruct{
	PrintFn: func(args ...interface{}) {
		fmt.Fprint(os.Stderr, args...)
	},
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
	return fmt.Sprintf("%s:%d: ", file, line)
}

func (l *LoggerStruct) Println(v ...interface{}) {
	if !l.disabled {
		var r []interface{}
		r = append(r, caller())
		var xs []string
		for _, x := range v {
			xs = append(xs, fmt.Sprint(x))
		}
		r = append(r, strings.Join(xs, " "))
		r = append(r, "\n")
		l.PrintFn(r...)
	}
}

func (l *LoggerStruct) Printf(format string, v ...interface{}) {
	if !l.disabled {
		l.PrintFn(fmt.Sprintf(caller()+format, v...))
	}
}

func (l *LoggerStruct) Fatal(v ...interface{}) {
	var r []interface{}
	r = append(r, caller())
	var xs []string
	for _, x := range v {
		xs = append(xs, fmt.Sprint(x))
	}
	r = append(r, strings.Join(xs, " "))
	r = append(r, "\n")
	l.PrintFn(r...)
	l.FlushFn()
	os.Exit(1)
}

func (l *LoggerStruct) Fatalf(format string, v ...interface{}) {
	l.PrintFn(fmt.Sprintf(caller()+format, v...))
	l.FlushFn()
	os.Exit(1)
}
