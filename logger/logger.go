package logger

import (
	"fmt"
	"strings"
	"sync"
)

var (
	// mu ensures that only one component writes to the terminal at a time.
	mu sync.Mutex

	// isInteractive tracks whether we are in a REPL mode.
	isInteractive bool

	// enabledCategories tracks which log categories are active.
	enabledCategories map[string]bool = map[string]bool{
		"system": true,
		"error":  true,
		"warn":   true,
		"player": true,
	}

	// customLogger is the actual implementation of the log output.
	// It defaults to standard fmt.Printf.
	customLogger func(string, ...interface{}) (int, error) = fmt.Printf
)

// SetInteractive enables or disables interactive mode for the logger.
func SetInteractive(interactive bool) {
	mu.Lock()
	defer mu.Unlock()
	isInteractive = interactive
}

// Enable activates specific log categories based on a colon-separated string (e.g. "player:server").
func Enable(categories string) {
	mu.Lock()
	defer mu.Unlock()
	if categories == "all" {
		enabledCategories["all"] = true
		return
	}
	parts := strings.Split(categories, ":")
	for _, p := range parts {
		if p != "" {
			enabledCategories[strings.ToLower(p)] = true
		}
	}
}

// SetLogger overrides the default fmt.Printf with a custom implementation.
func SetLogger(l func(string, ...interface{}) (int, error)) {
	mu.Lock()
	defer mu.Unlock()
	customLogger = l
}

// Printf prints a formatted message to the core logger (unfiltered).
func Printf(format string, a ...interface{}) (int, error) {
	mu.Lock()
	defer mu.Unlock()
	return customLogger(format, a...)
}

// Println is a convenience wrapper for Printf with a newline.
func Println(a ...interface{}) (int, error) {
	return Printf(fmt.Sprint(a...) + "\n")
}

// For returns a logger tied to a specific category.
func For(category string) CategorizedLogger {
	return CategorizedLogger{category: strings.ToLower(category)}
}

type CategorizedLogger struct {
	category string
}

func (cl CategorizedLogger) Printf(format string, a ...interface{}) (int, error) {
	mu.Lock()
	defer mu.Unlock()
	if !enabledCategories["all"] && !enabledCategories[cl.category] {
		return 0, nil
	}
	return customLogger(format, a...)
}

func (cl CategorizedLogger) Println(a ...interface{}) (int, error) {
	return cl.Printf(fmt.Sprint(a...) + "\n")
}

func (cl CategorizedLogger) Print(a ...interface{}) (int, error) {
	return cl.Printf("%s", fmt.Sprint(a...))
}
