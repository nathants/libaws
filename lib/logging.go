package lib

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

type LoggerStruct struct {
	Print    func(args ...any)
	Flush    func()
	disabled bool
}

var Logger = &LoggerStruct{
	Print: func(args ...any) {
		fmt.Fprint(os.Stderr, args...)
	},
	Flush:    func() {},
	disabled: strings.ToLower(os.Getenv("LOGGING") + " ")[:1] == "n",
}

func Caller(n int) string {
	_, file, line, _ := runtime.Caller(n)
	parts := strings.Split(file, "/")
	keep := []string{
		parts[len(parts)-2],
		parts[len(parts)-1],
	}
	file = strings.Join(keep, "/")
	return fmt.Sprintf("%s:%d: ", file, line)
}

func (l *LoggerStruct) Println(v ...any) {
	if !l.disabled {
		var r []any
		r = append(r, Caller(2))
		var xs []string
		for _, x := range v {
			xs = append(xs, fmt.Sprint(x))
		}
		r = append(r, strings.Join(xs, " "))
		r = append(r, "\n")
		l.Print(r...)
	}
}

func (l *LoggerStruct) Printf(format string, v ...any) {
	if !l.disabled {
		l.Print(fmt.Sprintf(Caller(2)+format, v...))
	}
}

func (l *LoggerStruct) Fatal(v ...any) {
	var r []any
	r = append(r, Caller(2))
	var xs []string
	for _, x := range v {
		xs = append(xs, fmt.Sprint(x))
	}
	r = append(r, strings.Join(xs, ""))
	r = append(r, "\n")
	l.Print(r...)
	l.Flush()
	os.Exit(1)
}

func (l *LoggerStruct) Fatalf(format string, v ...any) {
	l.Print(fmt.Sprintf(Caller(2)+format, v...))
	l.Flush()
	os.Exit(1)
}

type Debug struct {
	start time.Time
	name  string
}

func (d *Debug) Println(v ...any) {
	var r []any
	r = append(r, Caller(3))
	var xs []string
	for _, x := range v {
		xs = append(xs, fmt.Sprint(x))
	}
	r = append(r, strings.Join(xs, " "))
	r = append(r, "\n")
	Logger.Print(r...)
}

func (d *Debug) Start() {
	d.Println("DEBUG-START", d.name)
}

func (d *Debug) End() {
	d.Println("DEBUG-END", d.name, fmt.Sprint(int(time.Since(d.start).Milliseconds()))+"ms")
}
