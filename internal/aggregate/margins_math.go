package aggregate

import (
	"math"

	"abyssalauvreleasegate/internal/catalog"
	"abyssalauvreleasegate/internal/domain"
)

// trimReferenceDepthM is the reference depth (in metres) at which the AUV is
// nominally trimmed; waypoint depth excursions above or below it accumulate a
// signed trim-ballast requirement in grams.
const trimReferenceDepthM int64 = 2000

// trimScaleM is the depth-per-gram scale used to convert the accumulated depth
// excursion into a trim delta in grams.
const trimScaleM int64 = 100

// MarginComputation is the deterministic integer result of a return-safety
// margin check over the complete route.
type MarginComputation struct {
	EnergyUsedWh    int64 `json:"energy_used_wh"`
	ReturnReserveWh int64 `json:"return_reserve_wh"`
	PeakDepthM      int64 `json:"peak_depth_m"`
	PeakSpeedCmS    int64 `json:"peak_speed_cm_s"`
	TrimDeltaG      int64 `json:"trim_delta_g"`
}

// overflowError returns the stable integer-overflow validation envelope.
func overflowError(operation string, m domain.Mission) *domain.Error {
	return domain.NewError(domain.ErrCodeOverflow, operation).
		WithMission(m.MissionID, m.Generation, m.State).WithReasons("integer_overflow")
}

// computeMargins performs the bounded signed-64-bit arithmetic for depth,
// velocity, energy, return reserve, and buoyancy trim. It returns an overflow
// error (never partial values) before any evidence can be marked effective.
func computeMargins(m domain.Mission, waypoints []domain.Waypoint, hull catalog.AUVHull) (MarginComputation, error) {
	var comp MarginComputation
	var energy, trimNumerator int64
	var overflow bool

	for _, w := range waypoints {
		energy += w.ExpectedDrawWh
		if w.TargetDepthM > comp.PeakDepthM {
			comp.PeakDepthM = w.TargetDepthM
		}
		if w.MaxSpeedCmS > comp.PeakSpeedCmS {
			comp.PeakSpeedCmS = w.MaxSpeedCmS
		}
		excursion, ovf := sub64(w.TargetDepthM, trimReferenceDepthM)
		if ovf {
			return comp, overflowError("margins_check", m)
		}
		trimNumerator, overflow = add64(trimNumerator, excursion)
		if overflow {
			return comp, overflowError("margins_check", m)
		}
	}

	comp.EnergyUsedWh = energy
	reserve, ovf := sub64(hull.EnergyBudgetWh, energy)
	if ovf {
		return comp, overflowError("margins_check", m)
	}
	comp.ReturnReserveWh = reserve
	comp.TrimDeltaG = trimNumerator / trimScaleM
	return comp, nil
}

// evaluateMargins returns the sorted list of failing reason codes for a margin
// computation against the locked thresholds and hull capabilities.
func evaluateMargins(m domain.Mission, comp MarginComputation, hull catalog.AUVHull) []string {
	var reasons []string
	if comp.PeakDepthM > m.DepthLimitM {
		reasons = append(reasons, "depth_envelope_breach")
	}
	if comp.PeakDepthM > hull.MaxDepthM {
		reasons = append(reasons, "depth_beyond_hull")
	}
	if comp.PeakSpeedCmS > hull.MaxSpeedCmS {
		reasons = append(reasons, "speed_beyond_capability")
	}
	if comp.EnergyUsedWh > hull.EnergyBudgetWh {
		reasons = append(reasons, "energy_deficit")
	}
	if comp.ReturnReserveWh < m.ReturnThresholdWh {
		reasons = append(reasons, "return_reserve_deficit")
	}
	if comp.TrimDeltaG < m.TrimMinG || comp.TrimDeltaG > m.TrimMaxG {
		reasons = append(reasons, "trim_out_of_bounds")
	}
	return domain.SortedReasons(reasons...)
}

// add64 adds two signed 64-bit integers and reports overflow.
func add64(a, b int64) (int64, bool) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, true
	}
	return a + b, false
}

// sub64 subtracts two signed 64-bit integers and reports overflow.
func sub64(a, b int64) (int64, bool) {
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return 0, true
	}
	return a - b, false
}
