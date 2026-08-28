package libaws

import (
	"testing"
	"time"
)

func TestFormatS3VersionDateAcceptsAbbreviationOnlyLocalZone(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	value := time.Date(2026, 8, 28, 16, 24, 27, 123, time.UTC)
	if got := formatS3VersionDate(value, location); got != "2026-08-29 00:24:27" {
		t.Fatalf("unexpected local version date: %q", got)
	}
}
