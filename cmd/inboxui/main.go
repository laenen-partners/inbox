package main

import (
	"context"
	"crypto/rand"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laenen-partners/dsx"
	"github.com/laenen-partners/entitystore"
	appstatic "github.com/laenen-partners/inbox/cmd/inboxui/static"
	"github.com/laenen-partners/entitystore/store"
	"github.com/laenen-partners/inbox"
	inboxui "github.com/laenen-partners/inbox/ui"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	seed := flag.Bool("seed", false, "seed test data on startup")
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://localhost:5432/inbox?sslmode=disable"
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	if err := store.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	es, err := entitystore.New(entitystore.WithPgStore(pool))
	if err != nil {
		log.Fatalf("create entity store: %v", err)
	}

	ib := inbox.New(es)

	if *seed {
		if err := seedData(ctx, ib); err != nil {
			log.Fatalf("seed data: %v", err)
		}
		log.Println("test data seeded")
	}

	// Generate a random secret for CSRF tokens
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		log.Fatalf("generate secret: %v", err)
	}

	tokens := NewHMACTokens(secret)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(dsx.Middleware(dsx.MiddlewareConfig{Secret: secret}))

	// Serve dsx static assets (CSS, JS)
	staticFS, _ := fs.Sub(dsx.Static, "static")
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServerFS(staticFS)))

	// Serve custom theme CSS
	r.Handle("/theme/*", http.StripPrefix("/theme/", http.FileServerFS(appstatic.FS)))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/inbox", http.StatusFound)
	})
	r.Mount("/inbox", inboxui.Handler(ib,
		inboxui.WithBasePath("/inbox"),
		inboxui.WithLayout(showcaseLayout),
		inboxui.WithSigner(tokens, "http://localhost:8080/inbox/respond", 24*time.Hour),
		inboxui.WithVerifier(tokens),
		inboxui.WithActor(func(r *http.Request) string {
			// Check if actor was already set (e.g. by token middleware)
			if actor := inbox.ActorFrom(r.Context()); actor != "" {
				return actor
			}
			if actor := r.URL.Query().Get("actor"); actor != "" {
				return actor
			}
			return "user:operator"
		}),
		inboxui.WithFilter(inboxui.FilterConfig{
			Label: "Priority", TagPrefix: "priority:",
			Options: []string{"urgent", "high", "normal", "low"},
		}),
		inboxui.WithFilter(inboxui.FilterConfig{
			Label: "Team", TagPrefix: "team:",
			Options: []string{"compliance", "ops", "finance"},
		}),
		inboxui.WithFilter(inboxui.FilterConfig{
			Label: "Type", TagPrefix: "type:",
			Options: []string{"approval", "review", "input_required"},
		}),
	))

	log.Printf("inbox UI listening on %s", *addr)
	if err := http.ListenAndServe(*addr, r); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
