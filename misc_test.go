package rest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestLocation(t *testing.T) {
	if got := Location("v1", "ca", "abc"); got != "v1/ca/abc" {
		t.Errorf("Location = %q", got)
	}
	if got := Location(); got != "" {
		t.Errorf("Location() = %q, want empty", got)
	}
	if got := Location("only"); got != "only" {
		t.Errorf("Location = %q", got)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestGetFromBody(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	t.Run("valid", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"x","age":3}`))
		var p payload
		if err := GetFromBody(r, &p); err != nil {
			t.Fatal(err)
		}
		if p.Name != "x" || p.Age != 3 {
			t.Errorf("p = %+v", p)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{nope`))
		var p payload
		if err := GetFromBody(r, &p); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("read error", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", errReader{})
		var p payload
		if err := GetFromBody(r, &p); err == nil || err.Error() != "read failed" {
			t.Errorf("err = %v", err)
		}
	})
}

func TestLogResponseWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewLogResponseWriter(rec)

	w.WriteHeader(http.StatusTeapot)
	w.Write([]byte("abc"))
	w.Write([]byte("de"))

	if w.Status() != http.StatusTeapot || rec.Code != http.StatusTeapot {
		t.Errorf("status = %d / %d", w.Status(), rec.Code)
	}
	if w.Size() != 5 {
		t.Errorf("size = %d, want 5", w.Size())
	}
	if rec.Body.String() != "abcde" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestNewLogging(t *testing.T) {
	t.Run("debug level writes debug", func(t *testing.T) {
		l, path := newTestLogging(t)
		l.Debug("src", "dbg message", zap.String("k", "v"))
		l.Info("src", "info message")
		l.Error("src", "error message")
		l.Logger().Sync()

		out := readFile(t, path)
		for _, want := range []string{`"level":"debug"`, "dbg message", `"k":"v"`, `"source":"src"`, "info message", `"level":"error"`, "error message"} {
			if !strings.Contains(out, want) {
				t.Errorf("log output is missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("non-debug level drops debug", func(t *testing.T) {
		path := t.TempDir() + "/out.log"
		l, err := NewLogging([]string{path}, []string{path}, false)
		if err != nil {
			t.Fatal(err)
		}
		l.Debug("src", "dbg message")
		l.Info("src", "info message")
		l.Logger().Sync()

		out := readFile(t, path)
		if strings.Contains(out, "dbg message") {
			t.Errorf("debug must be dropped:\n%s", out)
		}
		if !strings.Contains(out, "info message") {
			t.Errorf("info expected:\n%s", out)
		}
	})

	t.Run("defaults to stderr", func(t *testing.T) {
		l, err := NewLogging(nil, nil, false)
		if err != nil || l.Logger() == nil {
			t.Fatalf("l = %v, err = %v", l, err)
		}
	})

	t.Run("invalid output path", func(t *testing.T) {
		if _, err := NewLogging([]string{"/nonexistent-dir/x/out.log"}, nil, false); err == nil {
			t.Error("expected an error")
		}
	})
}
