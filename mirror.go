package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/follower"
	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/spoolsync"
)

// mirrorEvery is how often the printers are reported to SpoolSync when no
// tool changed. A report that says what the last one said is not sent.
const mirrorEvery = 30 * time.Second

// mirrorFirst is the wait before the first report, so the follower has read
// the printers.
const mirrorFirst = 5 * time.Second

// kickMirror asks for a report now, as a tool changed. It never blocks.
func (s *service) kickMirror() {
	select {
	case s.mirrorKick <- struct{}{}:
	default:
	}
}

// runMirror reports the printers the follower holds to SpoolSync, with their
// status, job and the spool in each slot, until ctx ends.
// A standalone module keeps the inventory itself and has no server to report to.
func (s *service) runMirror(ctx context.Context) {
	to, ok := s.ss.(mirror)
	if !ok {
		return
	}

	first := time.NewTimer(mirrorFirst)
	defer first.Stop()

	tick := time.NewTicker(mirrorEvery)
	defer tick.Stop()

	var last string

	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
		case <-tick.C:
		case <-s.mirrorKick:
		}

		reports := s.printerReports()
		if len(reports) == 0 {
			continue
		}

		body, _ := json.Marshal(reports)
		if string(body) == last {
			continue
		}

		results, err := to.SyncPrinters(ctx, reports)
		if err != nil {
			s.log.Warn("could not report the printers to SpoolSync", "error", err)

			continue
		}

		for _, r := range results {
			if r.Error != "" {
				s.log.Warn("SpoolSync refused a printer", "device", r.ConnectID, "error", r.Error)
			}
		}

		last = string(body)
	}
}

// printerReports is every printer the follower holds, as SpoolSync takes it.
func (s *service) printerReports() []spoolsync.PrinterReport {
	var out []spoolsync.PrinterReport

	for _, p := range s.fo.Printers() {
		entries, _ := s.fo.Tools(p.ID)
		slots, active := slotsOf(entries)

		r := spoolsync.PrinterReport{
			ConnectID: p.ID, Name: p.Name, Serial: p.Serial, Model: p.Model,
			Status: statusOf(p), ActiveTool: active, Slots: slots,
		}

		if p.Job != nil && (p.State == "PRINTING" || p.State == "PAUSED") {
			r.Job = &spoolsync.JobReport{Name: p.Job.Name, Progress: p.Job.Progress, TimeRemaining: p.Job.TimeRemaining}
		}

		out = append(out, r)
	}

	return out
}

// statusOf is a printer's state in SpoolSync's terms.
func statusOf(p follower.Printer) string {
	switch {
	case !p.Connected:
		return "OFFLINE"
	case p.State == "PRINTING", p.State == "PAUSED", p.State == "ERROR":
		return p.State
	case p.State == "ATTENTION":
		return "ERROR"
	case p.State == "" || p.State == "UNKNOWN":
		return "UNKNOWN"
	default:
		return "OPERATIONAL"
	}
}

// slotsOf numbers a printer's tools as SpoolSync's slots, and answers the
// active one. A printer with an MMU has its slots (0.0, 0.1, ...) numbered
// from 0; one without has its heads (0, 1, ...), as an XL does.
func slotsOf(entries []follower.ToolEntry) (slots []spoolsync.SlotReport, active *int) {
	feeder := false

	for _, e := range entries {
		if !e.Tool.ID.IsHead() {
			feeder = true
		}
	}

	slots = []spoolsync.SlotReport{}

	for _, e := range entries {
		id := string(e.Tool.ID)
		if e.Tool.ID.IsHead() == feeder {
			continue
		}

		n, err := strconv.Atoi(id[strings.LastIndex(id, ".")+1:])
		if err != nil {
			continue
		}

		var spool *string
		if e.Binding.SpoolID != "" {
			spool = &e.Binding.SpoolID
		}

		slots = append(slots, spoolsync.SlotReport{Index: n, SpoolID: spool})

		if e.Tool.Active != nil && *e.Tool.Active {
			active = &n
		}
	}

	return slots, active
}
