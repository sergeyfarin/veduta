// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

// LimitDiff is one limit field whose requested value now exceeds what is currently approved -
// docs/01-architecture.md section 6's approval preview: "raisedLimits[]".
type LimitDiff struct {
	Field     string `json:"field"`
	Current   int    `json:"current"`
	Requested int    `json:"requested"`
}

// Diff is the permission diff shown before approval - the exact shape docs/01-architecture.md
// section 6 names: "{ addedRoutes[], removedRoutes[], addedCapabilities[], raisedLimits[],
// bodyBearingRoutes[] }". It is computed by exact-tuple comparison ("the UI displays manifest ∩
// lock for human review, computed by exact-tuple comparison, which IS well defined") - never the
// three-independent-searches evaluation Broker.Authorize performs at request time; docs/01 draws
// that distinction explicitly and this type is the "well defined" side of it.
type Diff struct {
	AddedRoutes       []Route     `json:"addedRoutes"`
	RemovedRoutes     []Route     `json:"removedRoutes"`
	AddedCapabilities []string    `json:"addedCapabilities"`
	RaisedLimits      []LimitDiff `json:"raisedLimits"`
	BodyBearingRoutes []Route     `json:"bodyBearingRoutes"`
}

// Empty reports whether the diff represents no requested change at all - the load flow's "match"
// branch, where re-approval is unnecessary because nothing about the request differs from what
// is already approved.
func (d Diff) Empty() bool {
	return len(d.AddedRoutes) == 0 && len(d.RemovedRoutes) == 0 &&
		len(d.AddedCapabilities) == 0 && len(d.RaisedLimits) == 0
}

// identitySet computes the authority-tuple identity of every route, keyed for membership tests.
func identitySet(routes []Route, requestBodyKB int) (map[identity]bool, error) {
	set := make(map[identity]bool, len(routes))
	for _, r := range routes {
		id, err := r.identity(requestBodyKB)
		if err != nil {
			return nil, err
		}
		set[id] = true
	}
	return set, nil
}

// ComputeDiff compares what the manifest currently requests against entry, the existing lock
// record (nil if the integration has never been approved - every requested route, capability and
// limit then shows as new, exactly as it should for a first approval).
func ComputeDiff(m *Manifest, entry *LockEntry) (Diff, error) {
	manifestReqBodyKB := requested("requestBodyKB", m.Limits)

	var lockRoutes []Route
	var lockCaps []string
	var effective EffectiveLimits
	if entry != nil {
		lockRoutes = entry.Routes
		lockCaps = entry.Capabilities
		effective = entry.EffectiveLimits
	}
	lockReqBodyKB := effective.RequestBodyKB

	manifestSet, err := identitySet(m.Routes, manifestReqBodyKB)
	if err != nil {
		return Diff{}, err
	}
	lockSet, err := identitySet(lockRoutes, lockReqBodyKB)
	if err != nil {
		return Diff{}, err
	}

	lockCapSet := make(map[string]bool, len(lockCaps))
	for _, c := range lockCaps {
		lockCapSet[c] = true
	}

	var diff Diff
	for _, r := range m.Routes {
		id, err := r.identity(manifestReqBodyKB)
		if err != nil {
			return Diff{}, err
		}
		if !lockSet[id] {
			diff.AddedRoutes = append(diff.AddedRoutes, r)
		}
		if bodyBearing(r.Method) {
			diff.BodyBearingRoutes = append(diff.BodyBearingRoutes, r)
		}
	}
	for _, r := range lockRoutes {
		id, err := r.identity(lockReqBodyKB)
		if err != nil {
			return Diff{}, err
		}
		if !manifestSet[id] {
			diff.RemovedRoutes = append(diff.RemovedRoutes, r)
		}
	}
	for _, c := range m.Capabilities {
		if !lockCapSet[c] {
			diff.AddedCapabilities = append(diff.AddedCapabilities, c)
		}
	}
	for _, name := range limitFieldOrder {
		req := requested(name, m.Limits)
		cur := effective.field(name)
		if req > cur {
			diff.RaisedLimits = append(diff.RaisedLimits, LimitDiff{Field: name, Current: cur, Requested: req})
		}
	}
	return diff, nil
}
