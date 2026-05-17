package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogIncludesField(t *testing.T) {
	var buf bytes.Buffer
	l := NewWith(&buf)
	l.Info("hello", "k", "v")
	out := buf.String()
	if !strings.Contains(out, "hello") || !strings.Contains(out, `"k":"v"`) {
		t.Fatalf("missing fields: %s", out)
	}
}
