// Package ui prints the CLI's output, with colour when it's going to a terminal.
package ui

import (
	"fmt"
	"io"
	"os"
)

var (
	Out   io.Writer = os.Stdout
	Err   io.Writer = os.Stderr
	color           = isTerminal(os.Stdout) && os.Getenv("NO_COLOR") == ""
)

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func paint(code, s string) string {
	if !color {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

func Bold(s string) string  { return paint("1", s) }
func Dim(s string) string   { return paint("2", s) }
func Green(s string) string { return paint("32", s) }
func Red(s string) string   { return paint("31", s) }
func Cyan(s string) string  { return paint("36", s) }

// Status colours an HTTP status: green for 2xx, red otherwise.
func Status(code int) string {
	text := fmt.Sprintf("%d", code)
	if code >= 200 && code < 300 {
		return Green(text)
	}
	return Red(text)
}

func Printf(format string, args ...any) { fmt.Fprintf(Out, format, args...) }
func Errorf(format string, args ...any) { fmt.Fprintf(Err, Red("Error: ")+format+"\n", args...) }
