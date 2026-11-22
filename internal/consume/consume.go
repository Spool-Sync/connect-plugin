// Package consume works out how much filament a printer tool fed: the
// millimetres between two odometer readings, and their weight.
//
// Copied from extensions/materials/internal/consume in the prusa-connect
// repository, which is MIT licensed (https://github.com/prusa3d/prusa-connect).
// The one change: the diameter is given in millimetres, as SpoolSync stores
// it, and not in micrometres.
package consume

import (
	"math"
	"strings"
)

// The density and the diameter taken when the material names none.
const (
	defaultDensity  = 1.24
	defaultDiameter = 1.75
	// cubicMMPerCM3 turns a volume in cubic millimetres into the cubic
	// centimetres a density is given per.
	cubicMMPerCM3 = 1000
	// circleArea is the area of a circle of diameter 1.
	circleArea = math.Pi / 4
)

// densities are the densities by material type, in g/cm³.
//
//nolint:gochecknoglobals,mnd // a fixed table, read-only: the numbers are the table.
var densities = map[string]float64{
	"PLA": 1.24, "PETG": 1.27, "ASA": 1.07, "PC": 1.20, "PVB": 1.10,
	"ABS": 1.04, "HIPS": 1.04, "PP": 0.90, "FLEX": 1.21, "PA": 1.14,
}

// Delta is the millimetres fed between the previous reading of a tool's
// odometer and this one. A first reading or a changed odometer id is a
// reset: the printer started counting again, and nothing is credited. The id
// also catches a printer that restarted and fed past its old reading before
// it was read again.
//
// Under the same id a lower reading is stale, since the id changes at each
// reset: it is not a new count, and stale says to keep the reading before
// it. Without an id a lower reading is a reset.
//
// Delta reads an odometer_weight the same way: its grams are the delta of
// the weight under the same rules.
func Delta(prevE *float64, prevID *int64, e float64, id *int64) (mm float64, stale bool) {
	switch {
	case prevE == nil || (prevID == nil) != (id == nil) || (id != nil && *prevID != *id):
		return 0, false
	case e < *prevE:
		return 0, id != nil
	}

	return e - *prevE, false
}

// Grams is the weight of mm millimetres of filament. The density is the
// spool's, else the one for its type, else 1.24; the diameter is the spool's
// in millimetres, else 1.75.
func Grams(mm float64, density *float64, materialType string, diameterMM *float64) float64 {
	rho, ok := densities[strings.ToUpper(materialType)]
	if !ok {
		rho = defaultDensity
	}

	if density != nil {
		rho = *density
	}

	d := defaultDiameter
	if diameterMM != nil {
		d = *diameterMM
	}

	return mm * circleArea * d * d / cubicMMPerCM3 * rho
}
