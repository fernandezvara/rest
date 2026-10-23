package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julienschmidt/httprouter"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		name    string
		matcher []string
		path    string
		want    bool
	}{
		{"no matcher", nil, "/v1/ca/anything", true},
		{"blank matchers", []string{"", "", ""}, "/v1/ca/anything", true},
		{"wildcard", []string{"", ".*", ""}, "/v1/ca/anything", true},
		{"regex matches", []string{"", "", "^[a-z]+$"}, "/v1/ca/abc", true},
		{"regex does not match", []string{"", "", "^[a-z]+$"}, "/v1/ca/ab1", false},
		{"first segment checked", []string{"^v2$"}, "/v1/ca", false},
		{"matcher longer than the path", []string{"", "", "", "^x$"}, "/v1/ca", false},
		{"invalid regex never matches", []string{"([a-z"}, "/v1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if got := match("", tt.matcher, r); got != tt.want {
				t.Errorf("match = %v, want %v", got, tt.want)
			}
		})
	}
}

func newTestREST(t *testing.T, routes map[string]map[string]APIEndpoint) (*REST, string) {
	t.Helper()

	l, path := newTestLogging(t)
	rr, err := New("127.0.0.1:0", nil, nil, nil, false, l)
	if err != nil {
		t.Fatal(err)
	}
	rr.SetupRouter(routes)
	return rr, path
}

func okHandler(body string) APIHandler {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}
}

func TestRouterDispatch(t *testing.T) {
	rr, logPath := newTestREST(t, map[string]map[string]APIEndpoint{
		"GET": {
			"/items/:id": {
				Handler: func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
					Response(w, map[string]string{"id": ps.ByName("id")}, nil, http.StatusOK, "")
				},
				Matcher: []string{"", "^[0-9]+$"},
			},
		},
		"POST": {
			"/items/:id": {Handler: okHandler("posted")},
		},
	})

	t.Run("matching route", func(t *testing.T) {
		rec := serve(rr.router, http.MethodGet, "/items/42", "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"42"`) {
			t.Errorf("code = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("matcher rejects the request", func(t *testing.T) {
		rec := serve(rr.router, http.MethodGet, "/items/abc", "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, want 400", rec.Code)
		}
		if e := decodeError(t, rec); e.Reason != "route does not match" {
			t.Errorf("payload = %+v", e)
		}
	})

	t.Run("unknown route", func(t *testing.T) {
		rec := serve(rr.router, http.MethodGet, "/nope", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("code = %d, want 404", rec.Code)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		rec := serve(rr.router, http.MethodDelete, "/items/42", "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("code = %d, want 405", rec.Code)
		}
	})

	t.Run("hits are logged", func(t *testing.T) {
		rr.logger.Logger().Sync()
		out := readFile(t, logPath)
		for _, want := range []string{"HTTP route added", `"msg":"hit"`, `"uri":"/items/42"`, `"code":200`} {
			if !strings.Contains(out, want) {
				t.Errorf("log is missing %q:\n%s", want, out)
			}
		}
	})
}

func TestRouterSecurityAndCORSHeaders(t *testing.T) {
	rr, _ := newTestREST(t, map[string]map[string]APIEndpoint{
		"GET":  {"/status": {Handler: okHandler("ok")}},
		"POST": {"/status": {Handler: okHandler("ok")}},
	})

	rec := serve(rr.router, http.MethodGet, "/status", "")

	want := map[string]string{
		"Access-Control-Allow-Origin":  "*",
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"X-XSS-Protection":             "1; mode=block",
		"Access-Control-Allow-Headers": "Origin, X-Requested-With, Content-Type, Accept, X-Session-Token",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS must not be set without TLS, got %q", got)
	}

	methods := rec.Header().Get("Access-Control-Allow-Methods")
	for _, m := range []string{"OPTIONS", "GET", "POST"} {
		if !strings.Contains(methods, m) {
			t.Errorf("Access-Control-Allow-Methods = %q, missing %s", methods, m)
		}
	}
}

func TestRouterOptions(t *testing.T) {
	rr, _ := newTestREST(t, map[string]map[string]APIEndpoint{
		"GET": {"/status": {Handler: okHandler("ok")}},
	})

	rec := serve(rr.router, http.MethodOptions, "/status", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS headers expected on OPTIONS")
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "OPTIONS, GET" {
		t.Errorf("Access-Control-Allow-Methods = %q", got)
	}
}

func TestRouterHSTSWhenTLS(t *testing.T) {
	rr, _ := newTestREST(t, map[string]map[string]APIEndpoint{
		"GET": {"/status": {Handler: okHandler("ok")}},
	})
	rr.tls = true

	for _, method := range []string{http.MethodGet, http.MethodOptions} {
		rec := serve(rr.router, method, "/status", "")
		if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
			t.Errorf("%s: HSTS = %q", method, got)
		}
	}
}
