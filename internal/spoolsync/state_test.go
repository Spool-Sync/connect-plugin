package spoolsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const connectStateJSON = `{
	"generatedAt":"2026-11-23T10:00:00.000Z",
	"printers":[{"connectId":"d1","printerId":"p1","name":"Valve","slots":[
		{"index":0,"spoolId":"s1","updatedAt":"2026-11-23T09:00:00.000Z"},
		{"index":1,"spoolId":null,"updatedAt":"2026-11-23T09:30:00.000Z"}]}],
	"spools":[{"spoolId":"s1","filamentTypeId":"ft1","currentWeight_g":812.5,
		"status":"ACTIVE","archivedAt":null,"updatedAt":"2026-11-23T09:59:00.000Z"}]
}`

// The snapshot maps every field the module adopts, with a slot's own instant.
func TestConnectStateMapsTheFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ss_test" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		if r.URL.Path != "/api/v1/printers/connect-sync/state" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		_, _ = w.Write([]byte(connectStateJSON))
	}))
	defer srv.Close()

	c := newClient(t, srv)

	state, err := c.ConnectState(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if state.GeneratedAt.Format(time.RFC3339) != "2026-11-23T10:00:00Z" {
		t.Fatalf("generated %v", state.GeneratedAt)
	}

	if len(state.Printers) != 1 || state.Printers[0].ConnectID != "d1" || len(state.Printers[0].Slots) != 2 {
		t.Fatalf("printers %+v", state.Printers)
	}

	slot := state.Printers[0].Slots[0]
	if slot.Index != 0 || slot.SpoolID == nil || *slot.SpoolID != "s1" {
		t.Fatalf("slot %+v", slot)
	}

	if state.Printers[0].Slots[1].SpoolID != nil {
		t.Fatalf("an empty slot stayed filled: %+v", state.Printers[0].Slots[1])
	}

	if len(state.Spools) != 1 || state.Spools[0].GrossG != 812.5 || state.Spools[0].FilamentTypeID != "ft1" {
		t.Fatalf("spools %+v", state.Spools)
	}
}

// The socket sends the same snapshot as the GET, and a client that is refused
// is not silently followed.
func TestWatchConnectStateFollowsTheFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ss_test" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		if r.URL.Path != "/api/v1/printers/connect-sync/ws" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		if err = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"state",`+connectStateJSON[1:])); err != nil {
			return
		}

		<-r.Context().Done()
	}))
	defer srv.Close()

	c := newClient(t, srv)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var got ConnectState
	if err := c.WatchConnectState(ctx, func(s ConnectState) error {
		got = s

		cancel()

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if len(got.Printers) != 1 || got.Printers[0].Slots[0].SpoolID == nil || *got.Printers[0].Slots[0].SpoolID != "s1" {
		t.Fatalf("state %+v", got)
	}
}

// A server that refuses the token ends the watch with an error, so the caller
// retries instead of pretending it is following.
func TestWatchConnectStateRefusesABadToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	c.token = "ss_wrong"

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if err := c.WatchConnectState(ctx, func(ConnectState) error { return nil }); err == nil {
		t.Fatal("a refused socket was followed")
	}
}
