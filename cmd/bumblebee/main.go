package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/c0ze/bumblebee/internal/config"
	"github.com/c0ze/bumblebee/internal/router"

	// Register the transformers this binary ships.
	_ "github.com/c0ze/bumblebee/transform/passthrough"
)

var version = "v0.0.0"

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	handler, cleanup, err := router.New(cfg, version)
	if err != nil {
		log.Fatalf("router: %v", err)
	}
	defer cleanup()

	srv := &http.Server{Addr: cfg.Server.Addr, Handler: handler}

	go func() {
		log.Printf("bumblebee %s listening on %s", version, cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
