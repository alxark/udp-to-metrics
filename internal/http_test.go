package internal

import (
	"io"
	"log"
	"net"
	"testing"
)

func TestHTTPServerCloseReleasesListenerBeforeRun(t *testing.T) {
	server, err := NewHttpServer(log.New(io.Discard, "", 0), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Listen(); err != nil {
		t.Fatal(err)
	}
	addr := server.httpLis.Addr().String()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}

	replacement, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listener was not released: %v", err)
	}
	replacement.Close()
}
