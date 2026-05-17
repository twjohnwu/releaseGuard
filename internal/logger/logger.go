package logger

import (
	"io"
	"log/slog"
	"os"
)

type Logger struct{ *slog.Logger }

func New() *Logger { return NewWith(os.Stderr) }

func NewWith(w io.Writer) *Logger {
	return &Logger{slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))}
}
