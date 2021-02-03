package rest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"go.uber.org/zap"
)

// SetupRouter builds a router for the REST API endpoints
func (rr *REST) SetupRouter(routes map[string]map[string]APIEndpoint) {

	rr.optionsMappings = make(map[string][]string)

	for method, mappings := range routes {
		for route := range mappings {
			_, ok := rr.optionsMappings[route]
			if ok {
				rr.optionsMappings[route] = append(rr.optionsMappings[route], method)
			} else {
				rr.optionsMappings[route] = []string{method}
			}
		}
	}

	var router *httprouter.Router
	router = httprouter.New()

	for method, mappings := range routes {
		for route, endpoint := range mappings {

			localMethod := method             // ensure it will be logged
			localRoute := route               // ensure it will be logged
			localFunction := endpoint.Handler // function
			localMatcher := endpoint.Matcher  // ensure naming compliance (if defined)

			// wrapper will handle all common logic as:
			//  - instrumentation
			//  - logging
			//  - headers
			var wrapper func(w http.ResponseWriter, r *http.Request, ps httprouter.Params)

			wrapper = func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
				now := time.Now()
				if rr.tls {
					w.Header().Add("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
				}
				if match(localRoute, localMatcher, r) {
					ww := NewLogResponseWriter(w)
					rr.writeHeaders(w, r, localRoute)
					localFunction(ww, r, ps)

					rr.log(false, "hit", localMethod, localRoute, r.RequestURI, r.RemoteAddr, ww.Status(), ww.Size(), time.Since(now))
					return
				}

				BadRequest(w, r, "route does not match")

				rr.log(true, "hit", localMethod, localRoute, r.RequestURI, r.RemoteAddr, http.StatusBadRequest, 0, time.Since(now))

			}

			router.Handle(method, route, wrapper)

			rr.logger.Info("rest", "HTTP route added",
				zap.String("method", localMethod),
				zap.String("route", localRoute),
				zap.String("matchers", strings.Join(localMatcher, ",")),
			)

		}
	}

	// OPTIONS routes
	for route := range rr.optionsMappings {

		localRoute := route // ensure it will be logged
		localMethod := "OPTIONS"
		wrapper := func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {

			now := time.Now()
			if rr.tls {
				w.Header().Add("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			ww := NewLogResponseWriter(w)
			rr.writeHeaders(ww, r, localRoute)
			ww.WriteHeader(http.StatusOK)

			rr.log(false, "hit", localMethod, localRoute, r.RequestURI, r.RemoteAddr, ww.Status(), ww.Size(), time.Since(now))

		}

		router.Handle(localMethod, route, wrapper)

		rr.logger.Info("rest", "HTTP route added",
			zap.String("method", localMethod),
			zap.String("route", localRoute),
		)
	}

	// ensure not found and not allowed handlers are logged also
	router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		ErrorResponse(w, http.StatusNotFound, "")
		rr.log(false, "hit", r.Method, r.RequestURI, r.RequestURI, r.RemoteAddr, http.StatusNotFound, 0, time.Since(now))
	})
	router.MethodNotAllowed = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		ErrorResponse(w, http.StatusMethodNotAllowed, "")
		rr.log(false, "hit", r.Method, r.RequestURI, r.RequestURI, r.RemoteAddr, http.StatusMethodNotAllowed, 0, time.Since(now))
	})

	rr.router = router

}

type rawLogWriter struct {
	logger *zap.Logger
}

func (r *rawLogWriter) Write(p []byte) (n int, err error) {

	r.logger.Warn(string(p), zap.String("source", "rest"))

	return len(p), nil

}

// Start runs the REST API blocking execution
func (rr *REST) Start() error {

	var err error

	// graceful stop must be handled by the caller
	rr.httpServer = new(http.Server)
	rr.httpServer.Addr = rr.ipPort
	rr.httpServer.Handler = rr.router
	rr.httpServer.MaxHeaderBytes = 1 << 20

	// internal errors, mainly tls ones
	rr.httpServer.ErrorLog = log.New(&rawLogWriter{logger: rr.logger.Logger()}, "httpServer", 0)

	// certificate information? then add TLS configuration
	if len(rr.tlsCer) > 0 && len(rr.tlsKey) > 0 && len(rr.tlsCACert) > 0 {

		var (
			tlsConfig   tls.Config
			certificate tls.Certificate
		)

		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(rr.tlsCACert)

		tlsConfig.ClientCAs = caCertPool
		if rr.requireClientCert {
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
		tlsConfig.BuildNameToCertificate()

		rr.tls = true
		tlsConfig.MinVersion = tls.VersionTLS12
		tlsConfig.CurvePreferences = []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256}
		tlsConfig.PreferServerCipherSuites = true
		tlsConfig.CipherSuites = []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		}

		tlsConfig.NextProtos = []string{"h2", "http/1.1"}

		certificate, err = tls.X509KeyPair(rr.tlsCer, rr.tlsKey)
		if err != nil {
			return err
		}

		tlsConfig.Certificates = []tls.Certificate{certificate}

		rr.httpServer.TLSConfig = &tlsConfig

		return rr.httpServer.ListenAndServeTLS("", "")
	}

	return rr.httpServer.ListenAndServe()

}

// Shutdown tells the http server to stop gracefully
func (rr *REST) Shutdown() error {
	return rr.httpServer.Shutdown(context.Background())
}

// match is the local function that verifies
func match(route string, matcher []string, r *http.Request) bool {

	var (
		routeParts []string
	)

	// if there is no matcher to match against then all match
	if len(matcher) == 0 {
		return true
	}

	routeParts = strings.Split(r.URL.Path, "/")

	for i, m := range matcher {
		// do no check blank or .* routes since everything is already allowed
		if m != "" && m != ".*" {
			matched, err := regexp.MatchString(m, routeParts[i+1])
			if err != nil || matched == false {
				return false
			}
		}
	}

	return true

}

func (rr *REST) writeHeaders(w http.ResponseWriter, r *http.Request, route string) {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.Header().Add("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept, X-Session-Token")
	w.Header().Add("Access-Control-Allow-Methods", fmt.Sprintf("OPTIONS, %s", strings.Join(rr.optionsMappings[route], ", ")))
	w.Header().Add("X-Content-Type-Options", "nosniff")
	w.Header().Add("X-Frame-Options", "DENY")
	w.Header().Add("X-XSS-Protection", "1; mode=block")
}
