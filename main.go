// Command spool-sync-prusa-connect is the SpoolSync module for Prusa Connect: a
// process that follows every printer, keeps the weight of each spool on a tool
// in step with what the printer reports, and serves the routes the browser
// half draws on. SpoolSync is the only inventory; the module replaces the
// Materials extension and keeps Materials' routes, rules and events.
//
// # The routes
//
// Materials' route set, with the same paths, query parameters, bodies and
// answers, mounted at /api/v1/modules/<namespace>/ and described in
// openapi.json. faults.go lists every code a refusal carries. GET /status
// answers what the module is doing.
//
// # Following the printers
//
// Once configured, the process reads every printer and the host's event stream,
// as itself, under the observe permission module.yaml asks for. internal/follower
// applies what each printer reports of its tools to the spool on it.
//
// # Settings
//
// module.yaml declares every setting: the SpoolSync address, its token, and the
// number of spools the module lets SpoolSync hold. The host sends them on
// plugin.configure. Nothing here reads an environment variable.
package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"

	// The two drivers the database may need. sdk/moduledb imports none.
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/spool-sync/connect-plugin/internal/follower"
	"github.com/spool-sync/connect-plugin/internal/local"
	"github.com/spool-sync/connect-plugin/internal/spoolsync"
	"github.com/spool-sync/connect-plugin/internal/store"
)

// name is what this child reports when the host asks it what it is. The name is
// the manifest's, and the host holds the two to each other.
const name = "spool-sync-prusa-connect"

// version is module.yaml's version. scripts/release.sh overwrites it with
// -ldflags "-X main.version=...", so a dev build answers the version its
// archive's manifest carries, such as 0.1.0-dev.4.
var version = "0.2.0"

var release = extension.Version(version)

// defaultMaxSpools is module.yaml's default for max_spools.
const defaultMaxSpools = 2000

// openAPI is the fragment describing the routes, with every path relative to
// the mount. The host holds it to routesapi.ParseFragment at launch.
//
//go:embed openapi.json
var openAPI []byte

// migrations is the schema, one directory per engine, because the operator
// chooses the engine.
//
//go:embed migrations/sqlite/* migrations/postgres/*
var migrations embed.FS

// errHalfSpoolSync refuses a SpoolSync address without a token, or a token without one.
var errHalfSpoolSync = errors.New("spoolsync_url and spoolsync_token go together: set both to use SpoolSync, or neither to run standalone")

// standalone is whether the settings name no SpoolSync server.
func (s Settings) standalone() bool {
	return strings.TrimSpace(s.SpoolSyncURL) == "" && strings.TrimSpace(s.SpoolSyncToken) == ""
}

// errNoDatabase refuses a start without a database grant: the module has
// nowhere to keep its assignments, odometers and favourites.
var errNoDatabase = errors.New("the operator granted this module no database. Approve its needs.database request")

// Settings is what an operator writes under this instance's config block. The
// host merges it over the defaults module.yaml declares and sends the result on
// plugin.configure. A key this struct does not carry refuses the start.
type Settings struct {
	// SpoolSyncURL is the address of the SpoolSync server, without /api/v1.
	// Left empty with the token, the module runs standalone and keeps the
	// inventory in its own database.
	SpoolSyncURL string `json:"spoolsync_url"`
	// SpoolSyncToken is a SpoolSync API token (ss_...).
	SpoolSyncToken string `json:"spoolsync_token"`
	// MaxSpools is how many spools the module lets SpoolSync hold before it refuses another.
	MaxSpools int `json:"max_spools"`
}

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

	mod := &module{}
	defer mod.close()

	// The method set is filled after the session exists, because the follower
	// and the event stream call the host's doors on it.
	serves := wirerpc.Methods{}

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{Role: wirerpc.RoleChild, Methods: serves})
	if err != nil {
		return fmt.Errorf("build the session: %w", err)
	}

	maps.Copy(serves, methods(mod, conn))

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
func methods(mod *module, conn *wirerpc.Conn) wirerpc.Methods {
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
		// The settings the host merged, and the database the operator granted. The
		// database is opened and migrated here, so the module reports ready only on
		// a schema it has applied.
		wirerpc.MethodConfigure: wirerpc.ConfigureReporting(
			func(ctx context.Context, sent Settings, grants wirerpc.Grants) (wirerpc.Configured, error) {
				var client *spoolsync.Client

				if !sent.standalone() {
					if strings.TrimSpace(sent.SpoolSyncURL) == "" || strings.TrimSpace(sent.SpoolSyncToken) == "" {
						return wirerpc.Configured{}, errHalfSpoolSync
					}

					var err error
					if client, err = spoolsync.New(sent.SpoolSyncURL, sent.SpoolSyncToken, nil); err != nil {
						return wirerpc.Configured{}, err
					}
				}

				if grants.Database == nil {
					return wirerpc.Configured{}, errNoDatabase
				}

				db, report, err := store.Open(ctx, *grants.Database, migrations)
				if err != nil {
					// The cause first, and the grant's String leaves the password out.
					return wirerpc.Configured{}, fmt.Errorf("%w (the database is %s)", err, grants.Database)
				}

				maxSpools := sent.MaxSpools
				if maxSpools <= 0 {
					maxSpools = defaultMaxSpools
				}

				log := slog.New(slog.NewTextHandler(os.Stderr, nil))
				st := store.New(db, grants.Database.Engine)
				subscribe := func(ctx context.Context, p busapi.SubscribeParams, onWindow func(busapi.Window) error) error {
					return busapi.Subscribe(ctx, conn, p, onWindow)
				}

				// The inventory is SpoolSync's, or the module's own when it runs standalone.
				var inv inventory = client

				where := sent.SpoolSyncURL
				if client == nil {
					inv, where = local.New(db, grants.Database.Engine, log), "its own database"
				}

				svc := newService(inv, st, log, maxSpools, accessapi.NewClient(conn), busapi.NewClient(conn))
				fo := follower.New(deviceapi.NewClient(conn), subscribe, inv, st, log)
				svc.fo = fo
				fo.Notify(svc.emitTool, svc.emitSpoolWritten)
				fo.OnTagged(svc.linkTagged)

				mod.swap(db, svc.routes(), func(ctx context.Context) {
					go svc.runMirror(ctx)
					go svc.runSyncBack(ctx)
					go svc.firstCatalogue(ctx)

					_ = fo.Run(ctx)
				})

				fmt.Fprintf(os.Stderr, "%s keeps its data in %s at schema version %d and writes to %s\n",
					name, grants.Database, report.Version, where)

				return wirerpc.Configured{Database: report.State()}, nil
			}),
		wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) {
			fmt.Fprintf(os.Stderr, "%s was asked to stop\n", name)

			mod.close()

			return nil, nil
		},
		routesapi.MethodServe: routesapi.Serve(mod),
	}
}

// module is the database, the routes and the follower plugin.configure opened.
// A second configure replaces them: the follower still at work is stopped, and
// waited for, before its database is closed.
type module struct {
	mu     sync.Mutex
	db     *sql.DB
	served http.Handler
	// stop stops the follower and waits for it to end.
	stop func()
}

// swap installs the next database, routes and follower in place of the last.
func (m *module) swap(db *sql.DB, served http.Handler, run func(context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		run(ctx)
	}()

	next := func() { cancel(); <-done }

	m.mu.Lock()
	oldDB, oldStop := m.db, m.stop
	m.db, m.served, m.stop = db, served, next
	m.mu.Unlock()

	if oldStop != nil {
		oldStop()
	}

	if oldDB != nil {
		_ = oldDB.Close()
	}
}

// close stops the follower and closes the database. It is safe to call twice.
func (m *module) close() { m.swap(nil, nil, func(context.Context) {}) }

// ServeHTTP hands the request to the routes on the open database. The host
// configures before the first request, so none arrives before them.
func (m *module) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	served := m.served
	m.mu.Unlock()

	if served == nil {
		served = unconfigured
	}

	served.ServeHTTP(w, r)
}

// unconfigured answers the module before its settings arrive: its status says so,
// and every other route is refused until then.
var unconfigured = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/status" {
		answer(w, http.StatusOK, map[string]any{"configured": false})

		return
	}

	refuse(w, NotConfigured, "", nil)
})

// stdio is the pair the host handed over. Close shuts the reading half, which
// is what ends a session whose host has gone quiet.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
