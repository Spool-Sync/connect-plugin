// Package syncer pushes the remaining weight of every spool a printer read off
// its NFC tag from Materials to SpoolSync, one way.
//
// A Materials spool the printer reported carries the tag's OpenPrintTag
// instance_uuid as its uuid. SpoolSync finds a spool by the same uuid. A spool
// added by hand in Materials has a uuid of Materials' own, which SpoolSync
// never holds, so it is skipped.
package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/spoolsync"
)

// SourcePrinter is the source Materials gives a spool read off an NFC tag.
const SourcePrinter = "printer"

// pageSize is how many spools one read of Materials asks for.
const pageSize = 100

// resyncEvery is how often every spool is read again, so a spool ingested in
// SpoolSync after its last event is found without waiting for it to print.
const resyncEvery = 15 * time.Minute

// SpoolSync is what the syncer needs of the SpoolSync client.
type SpoolSync interface {
	FindByInstanceUUID(ctx context.Context, uuid string) (spoolsync.Spool, error)
	SetWeight(ctx context.Context, spoolID string, grossG float64) error
}

// Caller makes a call to another module, as routesapi.Client does.
type Caller interface {
	Call(ctx context.Context, params routesapi.CallParams) (routesapi.Response, error)
}

// Subscribe opens the host's event stream, as busapi.Subscribe does on the session.
type Subscribe func(ctx context.Context, params busapi.SubscribeParams, onWindow func(busapi.Window) error) error

// Stats is what the status route shows.
type Stats struct {
	Pushed     int       `json:"pushed"`
	Unknown    int       `json:"unknown"`
	Failed     int       `json:"failed"`
	LastPush   time.Time `json:"last_push,omitzero"`
	LastError  string    `json:"last_error,omitempty"`
	LastSyncAt time.Time `json:"last_full_sync,omitzero"`
}

// Syncer follows Materials and writes to SpoolSync.
type Syncer struct {
	materials string
	call      Caller
	subscribe Subscribe
	ss        SpoolSync
	log       *slog.Logger
	retry     time.Duration
	resync    time.Duration

	mu      sync.Mutex
	grams   map[string]int64    // the whole grams last pushed, by instance uuid
	unknown map[string]struct{} // instance uuids SpoolSync did not hold at the last ask
	stats   Stats
}

// New returns a syncer of the Materials module with this namespace.
func New(materials string, call Caller, subscribe Subscribe, ss SpoolSync, log *slog.Logger) *Syncer {
	return &Syncer{
		materials: materials, call: call, subscribe: subscribe, ss: ss, log: log,
		retry: 30 * time.Second, resync: resyncEvery,
		grams: map[string]int64{}, unknown: map[string]struct{}{},
	}
}

// Stats is a copy of what the syncer has done.
func (s *Syncer) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.stats
}

// Run reads every spool once, then follows the spool events until ctx ends.
// A lost stream, or one the host says lost continuity, reads every spool
// again before following on.
func (s *Syncer) Run(ctx context.Context) error {
	var cursor string

	go s.every(ctx)

	for ctx.Err() == nil {
		if cursor == "" {
			if err := s.FullSync(ctx); err != nil {
				s.fail("read Materials", err)

				if !sleep(ctx, s.retry) {
					break
				}

				continue
			}
		}

		err := s.subscribe(ctx, busapi.SubscribeParams{
			Types:  []string{s.materials + ".spool"},
			Resume: cursor,
		}, func(w busapi.Window) error {
			if w.Resync {
				if err := s.FullSync(ctx); err != nil {
					return err
				}
			}

			for _, raw := range append(w.Events, w.Telemetry...) {
				s.handle(ctx, raw)
			}

			if w.Cursor != "" {
				cursor = w.Cursor
			}

			return nil
		})
		if ctx.Err() != nil {
			break
		}

		if err != nil {
			s.fail("follow the event stream", err)
			// The cursor may be past the replay window; start from a full read.
			cursor = ""
		}

		if !sleep(ctx, s.retry) {
			break
		}
	}

	return ctx.Err()
}

// every reads every spool again on a timer.
func (s *Syncer) every(ctx context.Context) {
	t := time.NewTicker(s.resync)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.FullSync(ctx); err != nil {
				s.fail("read Materials", err)
			}
		}
	}
}

// spoolEvent is the data of a Materials spool event.
type spoolEvent struct {
	SpoolUUID        string  `json:"spool_uuid"`
	RemainingWeightG float64 `json:"remaining_weight_g"`
	Deleted          bool    `json:"deleted"`
}

func (s *Syncer) handle(ctx context.Context, raw json.RawMessage) {
	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}

	if json.Unmarshal(raw, &env) != nil || env.Type != s.materials+".spool" {
		return
	}

	var ev spoolEvent
	if json.Unmarshal(env.Data, &ev) != nil || ev.Deleted || ev.SpoolUUID == "" {
		return
	}

	s.Push(ctx, ev.SpoolUUID, ev.RemainingWeightG)
}

// materialsSpool is what the syncer reads of GET /spools in Materials.
type materialsSpool struct {
	UUID             string  `json:"uuid"`
	Source           string  `json:"source"`
	RemainingWeightG float64 `json:"remaining_weight_g"`
	ArchivedAt       *string `json:"archived_at"`
}

// FullSync pushes every live spool Materials holds that a printer read off a tag.
func (s *Syncer) FullSync(ctx context.Context) error {
	// Ask SpoolSync once more about the spools it did not hold.
	s.mu.Lock()
	clear(s.unknown)
	s.mu.Unlock()

	for offset := 0; ; {
		q := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(pageSize)}}

		resp, err := s.call.Call(ctx, routesapi.CallParams{
			Module: s.materials, Method: http.MethodGet, Path: "/spools", Query: q.Encode(),
		})
		if err != nil {
			return fmt.Errorf("list the spools: %w", err)
		}

		if resp.Status != http.StatusOK {
			return fmt.Errorf("list the spools: Materials answered %d", resp.Status)
		}

		var page struct {
			Spools []materialsSpool `json:"spools"`
			Next   *int             `json:"next"`
		}

		if err = json.Unmarshal(resp.Body, &page); err != nil {
			return fmt.Errorf("list the spools: %w", err)
		}

		for _, sp := range page.Spools {
			if sp.Source == SourcePrinter && sp.ArchivedAt == nil {
				s.Push(ctx, sp.UUID, sp.RemainingWeightG)
			}
		}

		if page.Next == nil || *page.Next <= offset || len(page.Spools) == 0 {
			break
		}

		offset = *page.Next
	}

	s.mu.Lock()
	s.stats.LastSyncAt = time.Now().UTC()
	s.mu.Unlock()

	return nil
}

// Push sets the SpoolSync spool with this instance uuid to the filament left
// plus its empty spool, once per whole gram.
func (s *Syncer) Push(ctx context.Context, uuid string, remainingG float64) {
	grams := int64(math.Floor(max(0, remainingG)))

	s.mu.Lock()
	last, seen := s.grams[uuid]
	_, unknown := s.unknown[uuid]
	s.mu.Unlock()

	if unknown || (seen && last == grams) {
		return
	}

	sp, err := s.ss.FindByInstanceUUID(ctx, uuid)
	if errors.Is(err, spoolsync.ErrNotFound) {
		// Not ingested in SpoolSync (yet). Remember it so every gram does not ask again;
		// the next full sync asks once more.
		s.mu.Lock()
		s.unknown[uuid] = struct{}{}
		s.stats.Unknown = len(s.unknown)
		s.mu.Unlock()
		s.log.Debug("SpoolSync holds no spool with this instance uuid", "uuid", uuid)

		return
	}

	if err != nil {
		s.fail("find spool "+uuid, err)

		return
	}

	gross := max(0, remainingG) + sp.FilamentType.SpoolWeightG
	if math.Abs(gross-sp.CurrentWeightG) >= 0.5 {
		if err = s.ss.SetWeight(ctx, sp.ID, math.Round(gross*10)/10); err != nil {
			s.fail("set the weight of spool "+sp.ID, err)

			return
		}
	}

	s.mu.Lock()
	s.grams[uuid] = grams
	s.stats.Pushed++
	s.stats.LastPush = time.Now().UTC()
	s.mu.Unlock()
}

func (s *Syncer) fail(what string, err error) {
	s.mu.Lock()
	s.stats.Failed++
	s.stats.LastError = what + ": " + err.Error()
	s.mu.Unlock()
	s.log.Warn("sync failed", "step", what, "err", err)
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
