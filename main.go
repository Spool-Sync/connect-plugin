// Command spool-sync-prusa-connect keeps SpoolSync's spool weights in step
// with Prusa Connect. It follows the Materials module's spool events and, for
// every spool a printer read off its OpenPrintTag NFC tag, sets the weight of
// the SpoolSync spool with the same instance_uuid: the filament left plus the
// empty spool. One way only: nothing is written back to Prusa Connect.
//
// # The route
//
//	GET /status   what the sync has done: spools pushed, unknown to SpoolSync, failures
//
// The path is relative to the mount, so a caller reaches it at
// /api/v1/modules/<namespace>/status.
//
// # Settings
//
// module.yaml declares every setting: the SpoolSync address, its API token,
// and the namespace of the Materials module. The host sends them on
// plugin.configure, which (re)starts the sync loop.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/spoolsync"
	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/syncer"
)

// What this child reports when the host asks it what it is. The name is the
// manifest's, and the host holds the two to each other.
const (
	name    = "spool-sync-prusa-connect"
	release = extension.Version("0.1.0")
)

// defaultMaterials is the namespace of the Materials module from the official
// release. It is the default of the materials_module setting in module.yaml.
const defaultMaterials = "com.github.aux.prusa3d.materials"

// Settings is what an operator writes under this instance's config block. The
// host merges it over the defaults module.yaml declares and sends the result on
// plugin.configure. A key this struct does not carry refuses the start.
type Settings struct {
	// SpoolSyncURL is the address of the SpoolSync server, without /api/v1.
	SpoolSyncURL string `json:"spoolsync_url"`
	// SpoolSyncToken is a SpoolSync API token (ss_...) with spools:view and spools:edit.
	SpoolSyncToken string `json:"spoolsync_token"`
	// MaterialsModule is the namespace of the Materials module to follow.
	MaterialsModule string `json:"materials_module"`
}

// running is the sync loop plugin.configure started, stopped by the next one
// or by shutdown.
type running struct {
	mu     sync.Mutex
	stop   context.CancelFunc
	done   chan struct{}
	syncer atomic.Pointer[syncer.Syncer]
}

func (r *running) replace(s *syncer.Syncer) {
	r.halt()

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})

	r.mu.Lock()
	r.stop, r.done = stop, done
	r.mu.Unlock()
	r.syncer.Store(s)

	go func() {
		defer close(done)

		_ = s.Run(ctx)
	}()
}

func (r *running) halt() {
	r.mu.Lock()
	stop, done := r.stop, r.done
	r.stop, r.done = nil, nil
	r.mu.Unlock()

	if stop != nil {
		stop()
		<-done
	}
}

// openAPI is the fragment describing the route, with every path relative to
// the mount. The host holds it to routesapi.ParseFragment at launch and serves
// it rebased under the namespace, which is what cmd/generate-client-sdk reads
// to type this module's surface.
//
//go:embed openapi.json
var openAPI []byte

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

// run writes the handshake line and then serves frames until the host closes
// the channel.
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	line, err := wire.Handshake{
		Protocol: wire.ProtocolMajor,
		MinMinor: wire.MinMinor,
		MaxMinor: wire.MaxMinor,
		Address:  wire.StdioAddress,
	}.Line()
	if err != nil {
		return fmt.Errorf("render the handshake: %w", err)
	}

	if _, err = os.Stdout.WriteString(line); err != nil {
		return fmt.Errorf("write the handshake: %w", err)
	}

	loop := &running{}
	defer loop.halt()

	// The method set is filled after the session exists, because the sync
	// loop calls the host's bus and routes doors on it.
	serves := wirerpc.Methods{}

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{Role: wirerpc.RoleChild, Methods: serves})
	if err != nil {
		return fmt.Errorf("build the session: %w", err)
	}

	maps.Copy(serves, methods(loop, conn))

	// The pid is here because the host's log is the only place a person sees
	// this process. Nothing on the wire carries it.
	fmt.Fprintf(os.Stderr, "%s %s is serving on stdio as pid %d\n", name, release, os.Getpid())

	if err = conn.Serve(ctx); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// methods is the lifecycle set every child answers, plus the one call that
// carries this module's own HTTP surface.
func methods(loop *running, conn *wirerpc.Conn) wirerpc.Methods {
	return wirerpc.Methods{
		wirerpc.MethodInfo: func(context.Context, wirerpc.Call) (any, error) {
			return routesapi.Describe(
				wirerpc.Info{
					Name:     name,
					Version:  release,
					Contract: wirerpc.Contract,
					// Naming the call is what asks the host for the settings. A
					// module that names none is never configured.
					Serves: []wirerpc.Method{wirerpc.MethodConfigure},
				}, openAPI)
		},
		// The settings the host merged from module.yaml's defaults and this
		// instance's config block. A bad address or a missing token refuses
		// the configure, so the operator sees it on the module's page.
		wirerpc.MethodConfigure: wirerpc.Configure(func(_ context.Context, sent Settings) error {
			if sent.MaterialsModule == "" {
				sent.MaterialsModule = defaultMaterials
			}

			client, err := spoolsync.New(sent.SpoolSyncURL, sent.SpoolSyncToken, nil)
			if err != nil {
				return err
			}

			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			subscribe := func(ctx context.Context, p busapi.SubscribeParams, onWindow func(busapi.Window) error) error {
				return busapi.Subscribe(ctx, conn, p, onWindow)
			}

			loop.replace(syncer.New(sent.MaterialsModule, routesapi.NewClient(conn), subscribe, client, log))
			fmt.Fprintf(os.Stderr, "%s follows %s and writes to %s\n", name, sent.MaterialsModule, sent.SpoolSyncURL)

			return nil
		}),
		wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) {
			fmt.Fprintf(os.Stderr, "%s was asked to stop\n", name)

			loop.halt()

			return nil, nil
		},
		routesapi.MethodServe: routesapi.Serve(routes(loop)),
	}
}

// routes is the surface this module serves. The paths carry no prefix.
func routes(loop *running) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		s := loop.syncer.Load()
		if s == nil {
			answer(w, http.StatusOK, map[string]any{"configured": false})

			return
		}

		answer(w, http.StatusOK, map[string]any{"configured": true, "stats": s.Stats()})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		refuse(w, r, RouteNotFound.
			New("this module serves no route at "+r.URL.Path).
			With(fault.StringParam("path", r.URL.Path)))
	})

	return mux
}

// answer writes one JSON body, which is every route above.
func answer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(body)
}

// stdio is the pair the host handed over. Close shuts the reading half, which
// is what ends a session whose host has gone quiet.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
