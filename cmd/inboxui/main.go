package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laenen-partners/entitystore"
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

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Mount("/", inboxui.Handler(ib,
		inboxui.WithActor(func(r *http.Request) string {
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
