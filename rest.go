package rest

import (
	"crypto/tls"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/julienschmidt/httprouter"
	"go.uber.org/zap"
)

// REST holds all logic for the API defined
type REST struct {
	router            *httprouter.Router
	ipPort            string
	httpServer        *http.Server
	mu                sync.RWMutex // protects tlsCer, tlsKey and certificate
	certificate       *tls.Certificate
	optionsMappings   map[string][]string
	logger            *Logging
	tlsCer            []byte
	tlsKey            []byte
	tlsCACert         []byte
	tls               bool
	requireClientCert bool
}

// New returns a pointer to a REST struct that holds the interactions for the API
func New(ipPort string, tlsCertificate, tlsKey, tlsCACert []byte, requireClientCert bool, logger *Logging) (*REST, error) {

	var (
		rr  REST
		err error
	)

	rr.ipPort = ipPort
	rr.tlsCer = tlsCertificate
	rr.tlsKey = tlsKey
	rr.tlsCACert = tlsCACert
	rr.logger = logger
	rr.requireClientCert = requireClientCert

	// created here so Start, Shutdown and SetCertificate can be called from different goroutines
	rr.httpServer = &http.Server{
		Addr:           ipPort,
		MaxHeaderBytes: 1 << 20,
	}
	if logger != nil {
		// internal errors, mainly tls ones
		rr.httpServer.ErrorLog = log.New(&rawLogWriter{logger: logger.Logger()}, "httpServer", 0)
	}

	return &rr, err

}

func (rr *REST) log(isErr bool, msg, method, route, uri, remote string, httpErrorCode, size int, duration time.Duration) {

	if isErr {
		rr.logger.Error("rest", msg,
			zap.String("method", method),
			zap.String("uri", uri),
			zap.String("remote", remote),
			zap.Int("code", httpErrorCode),
			zap.Int("size", size),
			zap.Duration("duration", duration),
		)
		return
	}

	rr.logger.Info("rest", msg,
		zap.String("method", method),
		zap.String("uri", uri),
		zap.String("remote", remote),
		zap.Int("code", httpErrorCode),
		zap.Int("size", size),
		zap.Duration("duration", duration),
	)

}
