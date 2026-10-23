# rest

[![test](https://github.com/fernandezvara/rest/actions/workflows/test.yml/badge.svg)](https://github.com/fernandezvara/rest/actions/workflows/test.yml)

A small, opinionated library to simplify JSON API creation on top of
[httprouter](https://github.com/julienschmidt/httprouter) and [zap](https://github.com/uber-go/zap).

It takes care of the repetitive parts so handlers only deal with business logic:

- Routing from a plain map of `method -> route -> endpoint`.
- Per-route path validation with regular expressions (`Matcher`).
- Structured JSON access logging for every request.
- `OPTIONS` handling and CORS / security headers on every response (`X-Frame-Options`, `X-Content-Type-Options`, ...), plus HSTS when TLS is enabled.
- Consistent JSON error responses.
- Optional TLS with client certificate verification and live certificate rotation.

## Install

```sh
go get github.com/fernandezvara/rest
```

Requires Go 1.27.3 or later.

## Usage

```go
package main

import (
	"log"
	"net/http"

	"github.com/fernandezvara/rest"
	"github.com/julienschmidt/httprouter"
)

func main() {
	logger, err := rest.NewLogging([]string{"stdout"}, []string{"stderr"}, false)
	if err != nil {
		log.Fatal(err)
	}

	// no certificates: plain HTTP
	server, err := rest.New("127.0.0.1:8080", nil, nil, nil, false, logger)
	if err != nil {
		log.Fatal(err)
	}

	server.SetupRouter(map[string]map[string]rest.APIEndpoint{
		"GET": {
			"/items/:id": {
				Handler: getItem,
				// one entry per path segment: "items" is not checked, ":id" must be numeric
				Matcher: []string{"", "^[0-9]+$"},
			},
		},
	})

	// blocks until Shutdown is called, then returns http.ErrServerClosed
	if err := server.Start(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func getItem(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	rest.Response(w, map[string]string{"id": ps.ByName("id")}, nil, http.StatusOK, "")
}
```

### Responses

`rest.Response(w, body, err, status, location)` writes the body as JSON with the given status, or maps
`err` to an error response:

| error | status |
|---|---|
| `rest.ErrBadRequest` | 400 |
| `rest.ErrUnauthorized` | 401 |
| `rest.ErrNotFound` | 404 |
| `rest.ErrConflict` | 409 |
| `rest.ErrUnprocessableEntity` | 422 |
| anything else | 500, with the error text as `reason` |

Errors are returned as `{"code": 404, "message": "Not Found", "reason": "..."}`.
Helpers such as `rest.BadRequest`, `rest.BadRequestValidation`, `rest.Forbidden` and `rest.Unauthorized` write
the same payload directly, and `rest.GetFromBody(r, &obj)` decodes a JSON request body.

### TLS

Pass PEM-encoded certificate, key and CA to `rest.New` to serve HTTPS. Set `requireClientCert` to `true` to
require and verify client certificates (mutual TLS).

The certificate can be replaced while the server is running, no restart needed:

```go
err := server.SetCertificate(newCertPEM, newKeyPEM)
```

An invalid pair is rejected and the previous certificate keeps being served.

### Graceful shutdown

`Start` blocks. Call `Shutdown` from another goroutine (for example on `SIGINT`) to stop it gracefully.
`Start`, `Shutdown` and `SetCertificate` are safe to call from different goroutines.

## Development

```sh
go vet ./...
go test ./... -race -count=1
```

## License

MIT, see [LICENSE](LICENSE).
