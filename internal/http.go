package internal

import (
	"errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
)

type HttpServer struct {
	HttpPort int
	log      *log.Logger

	httpLis net.Listener
	httpMux *http.ServeMux
	httpSrv *http.Server
	mu      sync.Mutex
	closed  bool
}

func NewHttpServer(log *log.Logger, httpPort int) (hs *HttpServer, err error) {
	addr := "0.0.0.0:" + strconv.Itoa(httpPort)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	hs = &HttpServer{
		HttpPort: httpPort,
		log:      log,
		httpMux:  mux,
		httpSrv: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}

	return hs, nil
}

func (hs *HttpServer) Listen() error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if hs.httpLis != nil {
		return errors.New("HTTP server is already listening")
	}
	if hs.closed {
		return errors.New("HTTP server is closed")
	}

	lis, err := net.Listen("tcp", hs.httpSrv.Addr)
	if err != nil {
		return err
	}
	hs.httpLis = lis
	return nil
}

func (hs *HttpServer) Run() error {
	hs.mu.Lock()
	lis := hs.httpLis
	hs.mu.Unlock()
	if lis == nil {
		return errors.New("HTTP server is not listening")
	}

	hs.log.Printf("starting new HTTP instance on %s", hs.httpSrv.Addr)
	err := hs.httpSrv.Serve(lis)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (hs *HttpServer) Close() error {
	hs.mu.Lock()
	if hs.closed {
		hs.mu.Unlock()
		return nil
	}
	hs.closed = true
	lis := hs.httpLis
	hs.mu.Unlock()

	var listenerErr error
	if lis != nil {
		listenerErr = lis.Close()
		if errors.Is(listenerErr, net.ErrClosed) {
			listenerErr = nil
		}
	}
	if err := hs.httpSrv.Close(); err != nil {
		return err
	}
	return listenerErr
}
