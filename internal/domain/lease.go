package domain

// ResourceType enumerates the exclusively leased resources for a launch.
type ResourceType string

const (
	ResourceAUVHull         ResourceType = "auv_hull"
	ResourceBeaconSlot      ResourceType = "beacon_slot"
	ResourceRouteGeneration ResourceType = "route_generation"
)

// LeaseStatus is the lifecycle status of a lease row.
type LeaseStatus string

const (
	LeaseOpen     LeaseStatus = "open"
	LeaseReleased LeaseStatus = "released"
)

// Lease is a one-time effective reservation of an exclusive resource.
type Lease struct {
	LeaseID        string       `json:"lease_id"`
	ResourceType   ResourceType `json:"resource_type"`
	ResourceKey    string       `json:"resource_key"`
	MissionID      string       `json:"mission_id"`
	Generation     int64        `json:"generation"`
	Status         LeaseStatus  `json:"status"`
	AcquiredAtTick int64        `json:"acquired_at_tick"`
	ReleasedAtTick int64        `json:"released_at_tick"`
}
