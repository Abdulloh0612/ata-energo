package services

import (
	"ata-energo/pkg/deye"
	"testing"
)

func TestNormalizeDeyeCatalogStation(t *testing.T) {
	invalid := 200.0
	for raw, want := range map[string]string{"NORMAL": "normal", "PARTIAL_OFFLINE": "warning", "ALL_OFFLINE": "offline", "NO_DEVICE": "empty", "": "unknown"} {
		site := normalizeDeyeSite(deye.Station{ID: 73, RegionTimezone: "invalid/zone", ConnectionStatus: raw, LocationLat: &invalid})
		if site.ExternalID != "73" || site.Status != want || site.Timezone != "Asia/Bishkek" || site.Latitude != nil || site.Longitude != nil {
			t.Fatalf("unexpected normalization for %q: %+v", raw, site)
		}
	}
}
