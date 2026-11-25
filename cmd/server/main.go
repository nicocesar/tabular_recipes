// Command server is recipes.nicocesar.com: one binary with its templates,
// static files and fonts embedded (rule 1, web-layout.md).
//
// Environment:
//
//	PORT                    listen port (8080)
//	STORAGE                 "fs" (default, DATA_DIR) or "datastore" (GOOGLE_CLOUD_PROJECT)
//	DATA_DIR                filesystem store root (./data)
//	GOOGLE_CLOUD_PROJECT    Datastore project
//	ANTHROPIC_API_KEY       the project's own workspace key (ADR 0008); unset → runs fail fast
//	ANTHROPIC_WORKSPACE_ID  optional, for Console session links in Traces
//	WORK_URL                the service's own run.app URL; unset → runs are goroutines
//	TICK_TOKEN              /tick and /work credential
//	OWNER_PASSWORD          the owner's passphrase (ADR 0009)
//	SESSION_SECRET          signs the owner cookie and the per-IP cap key
//	DEMO_AGENT=1            local only, without ANTHROPIC_API_KEY: a free stand-in agent
package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nicocesar/tabular_recipes/internal/httpapi"
	"github.com/nicocesar/tabular_recipes/internal/store"
	"github.com/nicocesar/tabular_recipes/internal/tabulate"
)

//go:embed templates static
var assets embed.FS

//go:embed version.txt
var versionTxt []byte

// Set at link time (Dockerfile): facts about the build, not the source.
var (
	commit  string
	builtAt string
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	ctx := context.Background()
	var st store.Store
	switch env("STORAGE", "fs") {
	case "datastore":
		project := env("GOOGLE_CLOUD_PROJECT", os.Getenv("PROJECT_ID"))
		ds, err := store.NewDatastore(ctx, project)
		if err != nil {
			log.Fatalf("datastore: %v", err)
		}
		st = ds
	default:
		fs, err := store.NewFS(env("DATA_DIR", "./data"))
		if err != nil {
			log.Fatalf("fs store: %v", err)
		}
		st = fs
	}

	rn := &tabulate.Runner{
		Store:            st,
		WorkURL:          os.Getenv("WORK_URL"),
		TickToken:        os.Getenv("TICK_TOKEN"),
		ConsoleWorkspace: os.Getenv("ANTHROPIC_WORKSPACE_ID"),
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		rn.API = tabulate.NewClient(key)
		bctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := rn.Boot(bctx); err != nil {
			// Runs will fail with a clear trace; the site still serves.
			log.Printf("boot: %v", err)
			rn.API = nil
		}
		cancel()
	} else if os.Getenv("DEMO_AGENT") == "1" {
		rn.API = tabulate.NewDemo()
		rn.Boot(ctx)
		log.Printf("DEMO_AGENT=1: every run files the same demo card, for free")
	} else {
		log.Printf("ANTHROPIC_API_KEY unset: new runs will fail fast")
	}
	if rn.WorkURL != "" && rn.TickToken == "" {
		log.Printf("WORK_URL is set but TICK_TOKEN is not: dispatches will be refused")
	}

	srv, err := httpapi.New(httpapi.Config{
		Store: st, Runner: rn, Assets: assets,
		TickToken:     os.Getenv("TICK_TOKEN"),
		OwnerPassword: os.Getenv("OWNER_PASSWORD"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		Version:       strings.TrimSpace(string(versionTxt)),
		Commit:        commit,
		BuiltAt:       builtAt,
	})
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	// The boot resume pass: the same as a Tick, so a deploy picks up
	// anything it interrupted (ADR 0013). It only dispatches.
	go func() {
		time.Sleep(2 * time.Second)
		bctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if res := rn.Tick(bctx); len(res.Redispatched)+len(res.GaveUp) > 0 || len(res.Errors) > 0 {
			log.Printf("boot pass: %+v", res)
		}
	}()

	addr := ":" + env("PORT", "8080")
	log.Printf("recipes %s (%s) listening on %s", strings.TrimSpace(string(versionTxt)), env("COMMIT", commit), addr)
	log.Fatal(http.ListenAndServe(addr, srv))
}
