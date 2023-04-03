package internal

import (
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net"
	"net/http"
	"strconv"
)

type HttpServer struct {
	HttpPort int
	log      *log.Logger

	httpLis net.Listener
	httpMux *http.ServeMux
	httpSrv *http.Server
}

func NewHttpServer(log *log.Logger, httpPort int) (hs *HttpServer, err error) {
	hs = &HttpServer{}
	hs.log = log
	hs.HttpPort = httpPort

	return hs, nil
}

func (hs *HttpServer) Run() (err error) {
	addr := "0.0.0.0:" + strconv.Itoa(hs.HttpPort)

	lis, err := net.Listen(`tcp`, addr)
	if err != nil {
		return err
	}

	hs.httpLis = lis
	hs.httpMux = http.NewServeMux()
	hs.httpSrv = &http.Server{
		Addr: addr,
	}
	hs.httpMux.Handle("/metrics", promhttp.Handler())

	hs.httpSrv.Handler = hs.httpMux

	hs.log.Printf("starting new HTTP instance on %s", addr)
	return hs.httpSrv.Serve(hs.httpLis)
}
