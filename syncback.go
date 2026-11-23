package main

import (
	"context"
	"time"

	"github.com/spool-sync/connect-plugin/internal/follower"
	"github.com/spool-sync/connect-plugin/internal/spoolsync"
	"github.com/spool-sync/connect-plugin/internal/store"
)

// The feed of the SpoolSync state this module adopts. Its first try is at once;
// after a failure it waits longer each time, up to the ceiling. A server that
// answers the snapshot but not the socket (an older SpoolSync) is polled at
// pollEvery, so the feature still works without the feed.
const (
	firstWait = 2 * time.Second
	lastWait  = 30 * time.Second
	pollEvery = 60 * time.Second
)

// stateWatcher is the part of SpoolSync that says what is on its Connect
// printers' tools. A standalone module has nobody to ask and does not implement it.
type stateWatcher interface {
	ConnectState(ctx context.Context) (spoolsync.ConnectState, error)
	WatchConnectState(ctx context.Context, onState func(spoolsync.ConnectState) error) error
}

// runSyncBack follows SpoolSync's state until ctx ends. SpoolSync is where a
// person may assign a spool to a Connect printer's slot, clear one, or correct
// a spool's weight; this is what makes those changes reach the printer's Tools
// tab in Connect.
func (s *service) runSyncBack(ctx context.Context) {
	w, ok := s.ss.(stateWatcher)
	if !ok {
		return
	}

	wait := firstWait

	for {
		connected := false
		err := w.WatchConnectState(ctx, func(state spoolsync.ConnectState) error {
			connected = true

			return s.applyConnectState(ctx, state)
		})

		if ctx.Err() != nil {
			return
		}

		// The snapshot still works when the socket could not be opened, which
		// is how a SpoolSync without the feed is followed.
		polled := false
		if state, pullErr := w.ConnectState(ctx); pullErr == nil {
			if applyErr := s.applyConnectState(ctx, state); applyErr == nil {
				polled = true
			}
		}

		switch {
		case connected:
			// The socket worked and dropped: try it again soon.
			wait = firstWait
		case polled:
			// No socket, but the snapshot works: poll, do not hammer.
			wait = pollEvery
		default:
			wait = min(wait+wait, lastWait)
		}

		s.log.Warn("the SpoolSync feed could not be followed; trying again", "error", err, "wait", wait)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		wait = min(wait+wait, lastWait)
	}
}

// applyConnectState takes one SpoolSync snapshot: the spool in each slot of the
// printers this module follows, and the weight of the spools on them.
//
// A slot is a two-way value. The later of SpoolSync's change and the module's
// own assignment wins, so a person may assign in either UI without the other
// undoing it. A change made here after the snapshot is left for the module's
// next report, which carries the snapshot's instant as its reportedAt.
func (s *service) applyConnectState(ctx context.Context, state spoolsync.ConnectState) error {
	spools := make(map[string]spoolsync.ConnectSpool, len(state.Spools))
	for _, sp := range state.Spools {
		spools[sp.SpoolID] = sp
	}

	slots := make(map[string]map[int]spoolsync.ConnectSlot, len(state.Printers))
	for _, p := range state.Printers {
		byIndex := make(map[int]spoolsync.ConnectSlot, len(p.Slots))
		for _, slot := range p.Slots {
			byIndex[slot.Index] = slot
		}

		slots[p.ConnectID] = byIndex
	}

	changed := map[string]bool{}

	for _, p := range s.fo.Printers() {
		wanted, ok := slots[p.ID]
		if !ok {
			continue
		}

		entries, _ := s.fo.Tools(p.ID)
		feeder := hasFeeder(entries)

		for _, e := range entries {
			index, ok := slotIndex(e, feeder)
			if !ok {
				continue
			}

			slot, ok := wanted[index]
			if !ok {
				continue
			}

			local, assigned, err := s.st.Assignment(ctx, p.ID, string(e.Tool.ID))
			if err != nil {
				return err
			}

			if slot.SpoolID != nil {
				sp, known := spools[*slot.SpoolID]
				if !known {
					continue
				}

				if assigned && local.SpoolID == sp.SpoolID && local.FilamentTypeID == sp.FilamentTypeID {
					continue
				}

				// The module's own, later change is on its way to SpoolSync.
				if assigned && local.AssignedAt.After(slot.UpdatedAt) {
					continue
				}

				if err = s.adoptSlot(ctx, p, e, sp, slot.UpdatedAt); err != nil {
					return err
				}

				changed[p.ID] = true

				continue
			}

			if !assigned || !slot.UpdatedAt.After(local.AssignedAt) {
				continue
			}

			// SpoolSync cleared the slot after the module's assignment: take it off.
			if _, err = s.st.DeleteAssignment(ctx, p.ID, string(e.Tool.ID)); err != nil {
				return err
			}

			if err = s.st.DeleteOdometer(ctx, p.ID, string(e.Tool.ID)); err != nil {
				return err
			}

			changed[p.ID] = true
		}
	}

	for id := range changed {
		s.fo.Refresh(ctx, id)
	}

	s.applyWeights(ctx, state)
	s.rememberState(state.GeneratedAt)

	return nil
}

// adoptSlot puts a spool SpoolSync holds on a tool, and starts the count from
// what the tool reads now, as a person's assignment would.
func (s *service) adoptSlot(ctx context.Context, p follower.Printer, e follower.ToolEntry,
	sp spoolsync.ConnectSpool, at time.Time,
) error {
	a := store.Assignment{
		PrinterID: p.ID, Tool: string(e.Tool.ID), FilamentTypeID: sp.FilamentTypeID, SpoolID: sp.SpoolID,
		AssignedAt: at, AssignedBy: "spoolsync",
	}

	if err := s.st.SetAssignment(ctx, a); err != nil {
		return err
	}

	if e.Tool.OdometerE != nil {
		return s.st.SetOdometer(ctx, p.ID, string(e.Tool.ID), store.Odometer{
			ID: e.Tool.OdometerID, E: *e.Tool.OdometerE, Weight: e.Tool.OdometerWeight,
		}, time.Now())
	}

	return s.st.DeleteOdometer(ctx, p.ID, string(e.Tool.ID))
}

// applyWeights forgets the weight this module last knew of a spool SpoolSync
// changed itself, and drops the grams counted but not written: the correction
// supersedes them. A weight equal to the one this module last wrote is its own
// write coming back on the feed, not a correction, and is left alone.
func (s *service) applyWeights(ctx context.Context, state spoolsync.ConnectState) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	if s.weights == nil {
		s.weights = map[string]float64{}
	}

	for _, sp := range state.Spools {
		prev, seen := s.weights[sp.SpoolID]
		s.weights[sp.SpoolID] = sp.GrossG

		if !seen || prev == sp.GrossG {
			continue
		}

		if known, ok := s.fo.KnownGross(sp.SpoolID); ok && known == sp.GrossG {
			continue // this module wrote it
		}

		s.fo.SpoolChanged(sp.SpoolID)

		if err := s.st.ClearPending(ctx, sp.SpoolID); err != nil {
			s.log.Warn("could not drop the grams a SpoolSync weight change supersedes",
				"spool", sp.SpoolID, "error", err)
		}
	}
}

// rememberState keeps the instant of the newest snapshot, for the reportedAt a
// report carries so SpoolSync does not let an older report overwrite a change
// made after this snapshot.
func (s *service) rememberState(at time.Time) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	if at.After(s.syncAt) {
		s.syncAt = at
	}
}

// reportedAt is the instant of the state this module last adopted, nil before
// the first snapshot.
func (s *service) reportedAt() *time.Time {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	if s.syncAt.IsZero() {
		return nil
	}

	at := s.syncAt

	return &at
}

// hasFeeder says whether the printer has an MMU: its tools are then the
// feeders (0.0, 0.1, ...) and its heads (0) are not SpoolSync's slots.
func hasFeeder(entries []follower.ToolEntry) bool {
	for _, e := range entries {
		if !e.Tool.ID.IsHead() {
			return true
		}
	}

	return false
}

// slotIndex is the SpoolSync slot a tool is, and false for the head of an MMU
// printer and for an id that is not a number.
//
//nolint:nonamedreturns // the two answers are the point.
func slotIndex(e follower.ToolEntry, feeder bool) (index int, ok bool) {
	if e.Tool.ID.IsHead() == feeder {
		return 0, false
	}

	n, ok := parseSlot(string(e.Tool.ID))
	if !ok {
		return 0, false
	}

	return n, true
}
