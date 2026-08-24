package domain

// Mission is the aggregate root for a single deployment mission generation.
// Once locked, its fields are immutable except for the fields guarded by the
// aggregate (state, version, terminal result, timestamps).
type Mission struct {
	MissionID         string     `json:"mission_id"`
	VoyageID          string     `json:"voyage_id"`
	AUVHullID         string     `json:"auv_hull_id"`
	BeaconSlotID      string     `json:"beacon_slot_id"`
	Generation        int64      `json:"generation"`
	State             State      `json:"state"`
	LockedPayloadHash string     `json:"locked_payload_hash"`
	RouteHash         string     `json:"route_hash"`
	SeaStateRevision  int64      `json:"sea_state_revision"`
	ReturnThresholdWh int64      `json:"return_threshold_wh"`
	DepthLimitM       int64      `json:"depth_limit_m"`
	TrimMinG          int64      `json:"trim_min_g"`
	TrimMaxG          int64      `json:"trim_max_g"`
	AggregateVersion  int64      `json:"aggregate_version"`
	TerminalResult    string     `json:"terminal_result,omitempty"`
	CreatedAtTick     int64      `json:"created_at_tick"`
	UpdatedAtTick     int64      `json:"updated_at_tick"`
	Waypoints         []Waypoint `json:"waypoints,omitempty"`
}

// Waypoint is one ordered navigation segment target in a locked route package.
type Waypoint struct {
	MissionID         string `json:"mission_id"`
	Generation        int64  `json:"generation"`
	Seq               int64  `json:"seq"`
	LatitudeMicrodeg  int64  `json:"latitude_microdeg"`
	LongitudeMicrodeg int64  `json:"longitude_microdeg"`
	TargetDepthM      int64  `json:"target_depth_m"`
	MaxSpeedCmS       int64  `json:"max_speed_cm_s"`
	ExpectedDrawWh    int64  `json:"expected_draw_wh"`
	DwellSeconds      int64  `json:"dwell_seconds"`
	Checksum          string `json:"checksum"`
}
