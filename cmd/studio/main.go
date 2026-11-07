// Command studio runs the Micro Studio web workspace.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/micro/studio/internal/studio"
	"github.com/micro/studio/services/health"
	micro "go-micro.dev/v6"
	_ "go-micro.dev/v6/cmd/defaults"
	"go-micro.dev/v6/service"
)

func main() {
	address := os.Getenv("STUDIO_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		log.Fatal("STUDIO_ADDRESS must use a loopback IP, for example 127.0.0.1:8080")
	}
	s := micro.NewService("studio", service.Address("127.0.0.1:0"))
	s.Init()
	if err := s.Handle(&health.Health{}); err != nil {
		log.Fatal(err)
	}
	web := &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute}
	s.Init(service.BeforeStart(func() error {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		web.Handler = studio.New(s.Options().Registry, s.Client(), listener.Addr().String())
		log.Printf("Micro Studio: http://%s", listener.Addr())
		go func() {
			if err := web.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("studio: %v", err)
			}
		}()
		return nil
	}), service.BeforeStop(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return web.Shutdown(ctx)
	}))
	if err := s.Run(); err != nil {
		log.Fatal(err)
	}
}
