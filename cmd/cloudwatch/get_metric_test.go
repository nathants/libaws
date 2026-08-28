package libaws

import (
	"bytes"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

func TestWriteCloudwatchMetricDataAlignsSparseSeries(t *testing.T) {
	older := time.Date(2026, time.August, 28, 22, 41, 0, 0, time.UTC)
	newer := older.Add(time.Minute)
	results := []cwtypes.MetricDataResult{
		{
			Label:      aws.String("Invocations"),
			Timestamps: []time.Time{newer, older},
			Values:     []float64{2, 1},
		},
		{
			Label:      aws.String("Errors"),
			Timestamps: []time.Time{older},
			Values:     []float64{0},
		},
		{
			Label:      aws.String("Throttles"),
			Timestamps: []time.Time{newer, older},
			Values:     []float64{0, 0},
		},
	}

	var output bytes.Buffer
	if err := writeCloudwatchMetricData(
		&output,
		[]string{"Invocations", "Errors", "Throttles"},
		"FunctionName=better-beta",
		results,
	); err != nil {
		t.Fatal(err)
	}
	want := "timestamp=2026-08-28T22:41:00Z " +
		"FunctionName-better-beta::Invocations=1 " +
		"FunctionName-better-beta::Errors=0 " +
		"FunctionName-better-beta::Throttles=0 \n"
	if output.String() != want {
		t.Fatalf("sparse metric output mismatch:\n got: %q\nwant: %q", output.String(), want)
	}
}
