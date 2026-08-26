// Webhook receiver demo for OrbitProxy Event 隧道 (proxy type=webhook).
//
// Console Event 隧道 currently uses Forward mode: you run a local HTTP server,
// Connect the SDK Client, then set localAddr to that address when creating the tunnel.
//
//	export ORBITPROXY_AUTHTOKEN=...
//	export ORBITPROXY_MACHINE_KEY=...
//	export ORBITPROXY_API_URL=https://cp.orbitproxy.cloud   # or your CP base URL
//	# optional:
//	export WEBHOOK_LISTEN_ADDR=127.0.0.1:8080
//
//	go run ./examples/webhook-receiver
//
// Then in Console: Event Gateway → create Event 隧道 → bind this SDK Client,
// localAddr = WEBHOOK_LISTEN_ADDR (default 127.0.0.1:8080).
// Hit the public URL; logs print here; enable proxy_access_log on the tunnel to see Console access logs.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/orbitproxy/orbitproxy-go"
)

func main() {
	listenAddr := strings.TrimSpace(os.Getenv("WEBHOOK_LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = "127.0.0.1:8080"
	}

	// Getenv argument must be the env *name*, not the secret value.
	authToken := strings.TrimSpace(os.Getenv("ORBITPROXY_AUTHTOKEN"))
	machineKey := strings.TrimSpace(os.Getenv("ORBITPROXY_MACHINE_KEY"))
	apiURL := strings.TrimSpace(os.Getenv("ORBITPROXY_API_URL"))
	var missing []string
	if authToken == "" {
		missing = append(missing, "ORBITPROXY_AUTHTOKEN")
	}
	if machineKey == "" {
		missing = append(missing, "ORBITPROXY_MACHINE_KEY")
	}
	if apiURL == "" {
		missing = append(missing, "ORBITPROXY_API_URL")
	}
	if len(missing) > 0 {
		log.Fatalf("missing required env: %s\nset in shell (do not edit into Getenv):\n  export ORBITPROXY_AUTHTOKEN='...'\n  export ORBITPROXY_MACHINE_KEY='ck_...'\n  export ORBITPROXY_API_URL='https://cp.orbitproxy.cloud'",
			strings.Join(missing, ", "))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleWebhook)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("local webhook listening on http://%s (set Event 隧道 localAddr to this)", listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("local http: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc, err := orbitproxy.Connect(ctx, orbitproxy.Options{
		AuthToken:  authToken,
		MachineKey: machineKey,
		APIURL:     apiURL,
	})
	if err != nil {
		log.Fatalf("orbitproxy connect: %v", err)
	}
	defer svc.Close()

	log.Printf("orbitproxy connected machine_key=%s", svc.MachineKey())
	log.Printf("next: create Event 隧道 → machine=%s localAddr=%s → curl the public URL", svc.MachineKey(), listenAddr)

	<-svc.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	if err := svc.Err(); err != nil {
		log.Printf("service ended: %v", err)
	}
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	headers := map[string]string{}
	for name, values := range r.Header {
		headers[name] = strings.Join(values, ", ")
	}

	log.Printf("--- webhook ---")
	log.Printf("%s %s", r.Method, r.URL.RequestURI())
	log.Printf("remote=%s xff=%q", r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	log.Printf("content-type=%q content-length=%d", r.Header.Get("Content-Type"), len(body))
	if len(headers) > 0 {
		raw, _ := json.MarshalIndent(headers, "", "  ")
		log.Printf("headers=\n%s", raw)
	}
	if len(body) > 0 {
		log.Printf("body=%s", string(body))
	} else {
		log.Printf("body=<empty>")
	}
	log.Printf("elapsed=%s", time.Since(started).Round(time.Millisecond))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `{"ok":true,"method":%q,"path":%q,"bytes":%d}`+"\n",
		r.Method, r.URL.Path, len(body))
}
