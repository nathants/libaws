package lib

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

type LoggerStruct struct {
	Print    func(args ...interface{})
	Flush    func()
	disabled bool
}

var Logger = &LoggerStruct{
	Print: func(args ...interface{}) {
		fmt.Fprint(os.Stderr, args...)
	},
	Flush:    func() {},
	disabled: strings.ToLower(os.Getenv("LOGGING") + " ")[:1] == "n",
}

func caller(n int) string {
	_, file, line, _ := runtime.Caller(n)
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
		r = append(r, caller(2))
		var xs []string
		for _, x := range v {
			xs = append(xs, fmt.Sprint(x))
		}
		r = append(r, strings.Join(xs, " "))
		r = append(r, "\n")
		l.Print(r...)
	}
}

func (l *LoggerStruct) Printf(format string, v ...interface{}) {
	if !l.disabled {
		l.Print(fmt.Sprintf(caller(2)+format, v...))
	}
}

func (l *LoggerStruct) Fatal(v ...interface{}) {
	var r []interface{}
	r = append(r, caller(2))
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

func (l *LoggerStruct) Fatalf(format string, v ...interface{}) {
	l.Print(fmt.Sprintf(caller(2)+format, v...))
	l.Flush()
	os.Exit(1)
}

type Debug struct {
	start time.Time
	name  string
}

func (d *Debug) Println(v ...interface{}) {
	var r []interface{}
	r = append(r, caller(3))
	var xs []string
	for _, x := range v {
		xs = append(xs, fmt.Sprint(x))
	}
	r = append(r, strings.Join(xs, " "))
	r = append(r, "\n")
	Logger.Print(r...)
}

func (d *Debug) Log() {
	d.Println("DEBUG", d.name, fmt.Sprint(int(time.Since(d.start).Milliseconds()))+"ms")
}
