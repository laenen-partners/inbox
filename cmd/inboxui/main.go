package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laenen-partners/dsx/showcase"
	"github.com/laenen-partners/dsx/stream"
	"github.com/laenen-partners/dsx/ui/themecontroller"
	"github.com/laenen-partners/entitystore"
	"github.com/laenen-partners/entitystore/store"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
	appstatic "github.com/laenen-partners/inbox/cmd/inboxui/static"
	"github.com/laenen-partners/inbox/service"
	inboxtoken "github.com/laenen-partners/inbox/token"
	inboxui "github.com/laenen-partners/inbox/ui"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	port := flag.Int("port", 8080, "listen port")
	seed := flag.Bool("seed", false, "seed test data on startup")
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://localhost:5432/inbox?sslmode=disable"
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}

	es, err := entitystore.New(entitystore.WithPgStore(pool))
	if err != nil {
		return err
	}

	ib := inbox.New(es)

	if *seed {
		if err := seedData(ctx, ib); err != nil {
			return err
		}
		log.Println("test data seeded")
	}

	return showcase.Run(showcase.Config{
		Port: *port,
		Identities: []showcase.Identity{
			{Name: "Operator", TenantID: "demo", WorkspaceID: "demo", PrincipalID: "operator"},
			{Name: "Fatima (Compliance)", TenantID: "demo", WorkspaceID: "demo", PrincipalID: "compliance:fatima"},
			{Name: "Marco (Ops)", TenantID: "demo", WorkspaceID: "demo", PrincipalID: "ops:marco"},
			{Name: "Sarah (RM)", TenantID: "demo", WorkspaceID: "demo", PrincipalID: "rm:sarah"},
			{Name: "Customer CUST-1234", TenantID: "demo", WorkspaceID: "demo", PrincipalID: "customer:cust-1234"},
		},
		Setup: func(ctx context.Context, r chi.Router, broker *stream.Broker) error {
			// Serve custom theme CSS.
			r.Handle("/theme/*", http.StripPrefix("/theme/", http.FileServerFS(appstatic.FS)))

			// Theme persistence (toggle posts to /showcase/theme).
			r.Post("/showcase"+themecontroller.SetThemePath, themecontroller.SetThemeHandler(false))

			// Redirect root to inbox.
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/inbox", http.StatusFound)
			})

			// Presigned link handler.
			tokens := NewHMACTokens([]byte("showcase-secret"))
			r.Handle("/respond", inboxtoken.NewHandler(ib, tokens))

			// Create in-process Connect client for the inbox service.
			client := service.NewLocalClient(service.NewHandler(ib))

			// Mount inbox UI — identity is already set by showcase middleware.
			r.Mount("/inbox", inboxui.Handler(client,
				inboxui.WithBasePath("/inbox"),
				inboxui.WithLayout(showcaseLayout),
				inboxui.WithContentProvider("schema.v1.ItemSchema", schemaProvider{}),
				inboxui.WithContentProvider("inbox.v1.ItemSchema", schemaProvider{}),
				inboxui.WithIdentity(func(r *http.Request) identity.Context {
					id, _ := identity.FromContext(r.Context())
					return id
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

			return nil
		},
	})
}
