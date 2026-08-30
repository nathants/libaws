package libaws

import (
	"testing"
	"time"
)

func TestFormatS3VersionDateUsesLocalRFC3339Nano(t *testing.T) {
	original := time.Local
	time.Local = time.FixedZone("CST", 8*60*60)
	t.Cleanup(func() { time.Local = original })
	value := time.Date(2026, 8, 28, 16, 24, 27, 123, time.UTC)
	if got := formatS3VersionDate(value); got != "2026-08-29T00:24:27.000000123+08:00" {
		t.Fatalf("unexpected local version date: %q", got)
	}
}

func TestSortS3ObjectVersionsUsesExactInstantAcrossRepeatedLocalHour(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	original := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = original })

	older := time.Date(2024, 11, 3, 5, 50, 0, 0, time.UTC)
	newer := time.Date(2024, 11, 3, 6, 10, 0, 0, time.UTC)
	objects := []*S3ObjectVersion{
		{Key: "key", Version: "older", LastModified: older},
		{Key: "key", Version: "newer", LastModified: newer},
	}

	sortS3ObjectVersions(objects)
	if got := []string{objects[0].Version, objects[1].Version}; got[0] != "newer" || got[1] != "older" {
		t.Fatalf("version order = %v, want [newer older]", got)
	}
}
