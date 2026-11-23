package spoolsync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// ConnectState is what SpoolSync sends a module that follows it: the Connect
// printers it knows, the spool in each of their slots, and the load state of
// those spools. It is the reverse of the report a module pushes, so a person
// may assign a spool, clear a slot or correct a weight in SpoolSync's own UI
// and have the module follow.
//
// Every message is a full snapshot. GeneratedAt is when SpoolSync read it:
// a slot's UpdatedAt later than it is a change this module has not seen, and a
// report based on that snapshot must not overwrite it.
type ConnectState struct {
	GeneratedAt time.Time        `json:"generatedAt"`
	Printers    []ConnectPrinter `json:"printers"`
	Spools      []ConnectSpool   `json:"spools"`
}

// ConnectPrinter is one printer Prusa Connect follows, as SpoolSync holds it.
type ConnectPrinter struct {
	// ConnectID is the Connect device id, which the module's follower knows a
	// printer by.
	ConnectID string        `json:"connectId"`
	PrinterID string        `json:"printerId"`
	Name      string        `json:"name"`
	Slots     []ConnectSlot `json:"slots"`
}

// ConnectSlot is one slot of a Connect printer. SpoolID is nil for an empty
// slot. UpdatedAt is when SpoolSync last changed what the slot holds.
type ConnectSlot struct {
	Index     int       `json:"index"`
	SpoolID   *string   `json:"spoolId"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ConnectSpool is the load state of a spool on a Connect tool. GrossG is
// SpoolSync's gross weight: the filament and the empty spool.
type ConnectSpool struct {
	SpoolID        string     `json:"spoolId"`
	FilamentTypeID string     `json:"filamentTypeId"`
	GrossG         float64    `json:"currentWeight_g"`
	Status         string     `json:"status"`
	ArchivedAt     *time.Time `json:"archivedAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// ConnectState reads the snapshot once. It is what a client that cannot hold
// the socket (or one starting before the first message) uses.
func (c *Client) ConnectState(ctx context.Context) (ConnectState, error) {
	var out ConnectState
	if err := c.do(ctx, http.MethodGet, "/printers/connect-sync/state", nil, nil, &out); err != nil {
		return ConnectState{}, err
	}

	return out, nil
}

// WatchConnectState follows SpoolSync's feed until ctx ends or the connection
// fails, and calls onState for the first snapshot and every later one. A
// failure is returned so the caller can reconnect; ctx ending is not.
//
// The socket is authenticated with the same API token as every other call,
// sent in the Authorization header (never the URL, which ends up in logs). It
// is read-only: the server ignores what a client sends.
func (c *Client) WatchConnectState(ctx context.Context, onState func(ConnectState) error) error {
	header := http.Header{"Authorization": {"Bearer " + c.token}}

	// No c.http here: its Timeout bounds a request, and a socket is meant to
	// outlive one. The feed carries its own context and ping/pong keepalive.
	conn, resp, err := websocket.Dial(ctx, c.websocketURL(), &websocket.DialOptions{HTTPHeader: header})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	if err != nil {
		return fmt.Errorf("spoolsync: follow the feed: %w", err)
	}

	defer func() { _ = conn.CloseNow() }()

	// A snapshot holds every slot of every printer; a megabyte leaves room for
	// a large fleet and still bounds what a compromised server can make us hold.
	conn.SetReadLimit(maxBody)

	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return fmt.Errorf("spoolsync: read the feed: %w", err)
		}

		if kind != websocket.MessageText {
			continue
		}

		var msg struct {
			Type string `json:"type"`
			ConnectState
		}

		if err = json.Unmarshal(data, &msg); err != nil {
			return fmt.Errorf("spoolsync: decode the feed: %w", err)
		}

		if msg.Type != "state" {
			continue
		}

		if err = onState(msg.ConnectState); err != nil {
			return fmt.Errorf("spoolsync: apply the feed: %w", err)
		}
	}
}

// websocketURL is the feed address, with the scheme the base uses.
func (c *Client) websocketURL() string {
	u := strings.TrimSuffix(c.base, "/api/v1") + "/api/v1/printers/connect-sync/ws"

	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://")
	default:
		return u
	}
}
