package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/orbitproxy/orbitproxy-go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	id, err := orbitproxy.Register(ctx, orbitproxy.RegisterOptions{
		AuthToken: os.Getenv("ORBITPROXY_AUTHTOKEN"),
		ClientKey: os.Getenv("ORBITPROXY_CLIENT_KEY"),
		APIURL:    os.Getenv("ORBITPROXY_API_URL"),
	})
	if err != nil {
		log.Fatal(err)
	}

	svc, err := orbitproxy.Start(ctx, *id, orbitproxy.StartOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer svc.Close()

	fmt.Printf("started client_key=%s\n", svc.ClientKey())

	// Listen mode. Forward endpoints need no extra code.
	ln, err := svc.Listen(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello from orbitproxy listen\n"))
	})

	go func() {
		if err := http.Serve(ln, mux); err != nil && err != http.ErrServerClosed {
			log.Printf("serve: %v", err)
		}
	}()

	<-svc.Done()
	if err := svc.Err(); err != nil {
		log.Printf("service ended: %v", err)
	}
}
