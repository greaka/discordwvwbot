package loglevels

import (
	"fmt"
	"io"
	"log"
)

// Level describes the log level
type Level int

var (
	wInfo    *log.Logger
	wWarning *log.Logger
	wError   *log.Logger
)

// enum values for the log level
const (
	LevelInfo Level = iota
	LevelWarning
	LevelError
)

// Info writes to the info writer
func Info(p ...interface{}) {
	wInfo.Println(p...)
}

// Warning writes to the warning writer
func Warning(p ...interface{}) {
	wWarning.Println(p...)
}

// Error writes to the error writer
func Error(p ...interface{}) {
	wError.Println(p...)
}

// Infof calls fmt.Sprintf and writes the return to the info writer
func Infof(p string, v ...interface{}) {
	wInfo.Printf(fmt.Sprintf(p, v...))
}

// Warningf calls fmt.Sprintf and writes the return to the warning writer
func Warningf(p string, v ...interface{}) {
	wWarning.Printf(fmt.Sprintf(p, v...))
}

// Errorf calls fmt.Sprintf and writes the return to the error writer
func Errorf(p string, v ...interface{}) {
	wError.Printf(fmt.Sprintf(p, v...))
}

// SetWriter sets the writer to which a channel writes
func SetWriter(level Level, w io.Writer) {
	switch level {
	case LevelInfo:
		wInfo = log.New(w, "", log.LstdFlags|log.LUTC)
	case LevelWarning:
		wWarning = log.New(w, "", log.LstdFlags|log.LUTC)
	case LevelError:
		wError = log.New(w, "", log.LstdFlags|log.LUTC)
	}
}
