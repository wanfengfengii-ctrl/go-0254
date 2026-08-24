// Command server runs the Abyssal AUV release-gate HTTP service and serves the
// connected browser operations console.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"abyssalauvreleasegate/internal/aggregate"
	"abyssalauvreleasegate/internal/catalog"
	"abyssalauvreleasegate/internal/httpapi"
	"abyssalauvreleasegate/internal/store"
)

func main() {
	addr := flag.String("addr", envOr("AUVGATE_ADDR", ":8080"), "HTTP listen address")
	dbPath := flag.String("db", envOr("AUVGATE_DB", "auvgate.db"), "SQLite database path (or :memory:)")
	staticDir := flag.String("static", envOr("AUVGATE_STATIC", "web/dist"), "built browser console directory")
	flag.Parse()

	st, err := store.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := aggregate.New(catalog.DefaultSeed(), st, logicalClock(), newID)
	srv := httpapi.New(svc, *staticDir)

	log.Printf("abyssal auv release gate listening on %s (db=%s static=%s)", *addr, *dbPath, *staticDir)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

// logicalClock returns the current logical tick used for ordering events.
func logicalClock() func() int64 {
	return func() int64 { return time.Now().UnixNano() }
}

// newID returns a random 128-bit hex identifier.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b[:])
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
