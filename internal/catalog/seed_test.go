package catalog_test

import (
	"testing"

	"abyssalauvreleasegate/internal/catalog"
	"abyssalauvreleasegate/internal/domain"
)

func TestDefaultSeedLookups(t *testing.T) {
	c := catalog.DefaultSeed()

	if _, ok := c.Voyage("VGR-01"); !ok {
		t.Fatal("VGR-01 voyage should exist")
	}
	if _, ok := c.Voyage("MISSING"); ok {
		t.Fatal("MISSING voyage should not exist")
	}

	if !c.HullFitsVoyage("HULL-01", "VGR-01") {
		t.Error("HULL-01 should fit VGR-01")
	}
	if c.HullFitsVoyage("HULL-01", "OTHER") {
		t.Error("HULL-01 should not fit OTHER")
	}

	if !c.SeaStateCurrent("VGR-01", 1) {
		t.Error("sea-state revision 1 should be current for VGR-01")
	}
	if c.SeaStateCurrent("VGR-01", 99) {
		t.Error("sea-state revision 99 should not be current")
	}

	if _, ok := c.Reviewer("eng-alice", domain.RoleTechnicalAuthorizer); !ok {
		t.Error("eng-alice should be a qualified technical authorizer")
	}
	if _, ok := c.Reviewer("sea-carol", domain.RoleTechnicalAuthorizer); ok {
		t.Error("sea-carol should not be a technical authorizer")
	}

	if _, ok := c.BeaconSlot("BEACON-01"); !ok {
		t.Error("BEACON-01 beacon slot should exist")
	}
}
