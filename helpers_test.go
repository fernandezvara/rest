package rest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestLogging returns a Logging writing to a file inside the test temp dir
func newTestLogging(t *testing.T) (*Logging, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "out.log")
	l, err := NewLogging([]string{path}, []string{path}, true)
	if err != nil {
		t.Fatal(err)
	}
	return l, path
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// serve runs one request against the handler and returns the recorded response
func serve(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, rd))
	return rec
}
