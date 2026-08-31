package lib

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

type cloudwatchMetricDataCall struct {
	nextToken      string
	queryIDs       []string
	metricNames    []string
	dimensionNames []string
	dimensionVals  []string
}

type fakeCloudwatchMetricDataClient struct {
	pages []*cloudwatch.GetMetricDataOutput
	err   error
	calls []cloudwatchMetricDataCall
}

func (client *fakeCloudwatchMetricDataClient) GetMetricData(
	_ context.Context,
	input *cloudwatch.GetMetricDataInput,
	_ ...func(*cloudwatch.Options),
) (*cloudwatch.GetMetricDataOutput, error) {
	call := cloudwatchMetricDataCall{nextToken: aws.ToString(input.NextToken)}
	for _, query := range input.MetricDataQueries {
		call.queryIDs = append(call.queryIDs, aws.ToString(query.Id))
		if query.MetricStat == nil || query.MetricStat.Metric == nil {
			return nil, errors.New("test received query without a metric")
		}
		metric := query.MetricStat.Metric
		call.metricNames = append(call.metricNames, aws.ToString(metric.MetricName))
		if len(metric.Dimensions) != 1 {
			return nil, fmt.Errorf("test received %d dimensions", len(metric.Dimensions))
		}
		call.dimensionNames = append(call.dimensionNames, aws.ToString(metric.Dimensions[0].Name))
		call.dimensionVals = append(call.dimensionVals, aws.ToString(metric.Dimensions[0].Value))
	}
	client.calls = append(client.calls, call)
	if client.err != nil {
		return nil, client.err
	}
	pageIndex := len(client.calls) - 1
	if pageIndex >= len(client.pages) {
		return nil, fmt.Errorf("unexpected GetMetricData call %d", len(client.calls))
	}
	return client.pages[pageIndex], nil
}

func cloudwatchMetricDataTestRange() (*time.Time, *time.Time) {
	from := time.Date(2026, time.August, 30, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	return &from, &to
}

func TestCloudwatchGetMetricDataPaginatesWithStableQueries(t *testing.T) {
	from, to := cloudwatchMetricDataTestRange()
	older := from.Add(time.Minute)
	newer := older.Add(time.Minute)
	client := &fakeCloudwatchMetricDataClient{pages: []*cloudwatch.GetMetricDataOutput{
		{
			MetricDataResults: []cwtypes.MetricDataResult{
				{Id: aws.String("m1"), Label: aws.String("human label B"), Timestamps: []time.Time{newer}, Values: []float64{20}, StatusCode: cwtypes.StatusCodeComplete},
				{Id: aws.String("m0"), Label: aws.String("human label A"), Timestamps: []time.Time{newer}, Values: []float64{10}, StatusCode: cwtypes.StatusCodePartialData},
			},
			NextToken: aws.String("page-2"),
		},
		{
			MetricDataResults: []cwtypes.MetricDataResult{
				{Id: aws.String("m0"), Label: aws.String("changed label A"), Timestamps: []time.Time{older}, Values: []float64{1}, StatusCode: cwtypes.StatusCodeComplete},
			},
		},
	}}

	results, err := cloudwatchGetMetricData(
		context.Background(), client, 60, "Sum", from, to,
		"Test/Namespace", []string{"MetricA", "MetricB"}, "Dimension=value=with-equals",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if len(client.calls) != 2 {
		t.Fatalf("GetMetricData calls = %d, want 2", len(client.calls))
	}
	for i, call := range client.calls {
		if !slices.Equal(call.queryIDs, []string{"m0", "m1"}) {
			t.Fatalf("call %d query IDs = %v", i+1, call.queryIDs)
		}
		if !slices.Equal(call.metricNames, []string{"MetricA", "MetricB"}) {
			t.Fatalf("call %d metrics = %v", i+1, call.metricNames)
		}
		if !slices.Equal(call.dimensionNames, []string{"Dimension", "Dimension"}) {
			t.Fatalf("call %d dimension names = %v", i+1, call.dimensionNames)
		}
		if !slices.Equal(call.dimensionVals, []string{"value=with-equals", "value=with-equals"}) {
			t.Fatalf("call %d dimension values = %v", i+1, call.dimensionVals)
		}
	}
	if client.calls[0].nextToken != "" || client.calls[1].nextToken != "page-2" {
		t.Fatalf("pagination tokens = %q, %q", client.calls[0].nextToken, client.calls[1].nextToken)
	}
}

func TestCloudwatchGetMetricDataValidatesBeforeNetworking(t *testing.T) {
	from, to := cloudwatchMetricDataTestRange()
	for _, test := range []struct {
		name      string
		metrics   []string
		dimension string
	}{
		{name: "no metrics", dimension: "Name=value"},
		{name: "empty metric", metrics: []string{""}, dimension: "Name=value"},
		{name: "duplicate metric", metrics: []string{"Metric", "Metric"}, dimension: "Name=value"},
		{name: "missing separator", metrics: []string{"Metric"}, dimension: "invalid"},
		{name: "empty dimension name", metrics: []string{"Metric"}, dimension: "=value"},
		{name: "empty dimension value", metrics: []string{"Metric"}, dimension: "Name="},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeCloudwatchMetricDataClient{}
			var err error
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Errorf("validation panicked: %v", recovered)
					}
				}()
				_, err = cloudwatchGetMetricData(
					context.Background(), client, 60, "Sum", from, to,
					"Test/Namespace", test.metrics, test.dimension,
				)
			}()
			if err == nil {
				t.Fatal("invalid request unexpectedly succeeded")
			}
			if len(client.calls) != 0 {
				t.Fatalf("invalid request made %d GetMetricData calls", len(client.calls))
			}
		})
	}
}

func TestCloudwatchGetMetricDataRejectsIncompleteResponses(t *testing.T) {
	from, to := cloudwatchMetricDataTestRange()
	complete := func(id string) cwtypes.MetricDataResult {
		return cwtypes.MetricDataResult{Id: aws.String(id), StatusCode: cwtypes.StatusCodeComplete}
	}
	message := cwtypes.MessageData{Code: aws.String("DataError"), Value: aws.String("incomplete data")}
	for _, test := range []struct {
		name  string
		pages []*cloudwatch.GetMetricDataOutput
		want  string
	}{
		{
			name: "operation message",
			pages: []*cloudwatch.GetMetricDataOutput{{
				MetricDataResults: []cwtypes.MetricDataResult{complete("m0")},
				Messages:          []cwtypes.MessageData{message},
			}},
			want: "DataError",
		},
		{
			name: "result message",
			pages: []*cloudwatch.GetMetricDataOutput{{MetricDataResults: []cwtypes.MetricDataResult{{
				Id: aws.String("m0"), StatusCode: cwtypes.StatusCodeComplete, Messages: []cwtypes.MessageData{message},
			}}}},
			want: "DataError",
		},
		{
			name:  "forbidden",
			pages: []*cloudwatch.GetMetricDataOutput{{MetricDataResults: []cwtypes.MetricDataResult{{Id: aws.String("m0"), StatusCode: cwtypes.StatusCodeForbidden}}}},
			want:  "Forbidden",
		},
		{
			name:  "internal error",
			pages: []*cloudwatch.GetMetricDataOutput{{MetricDataResults: []cwtypes.MetricDataResult{{Id: aws.String("m0"), StatusCode: cwtypes.StatusCodeInternalError}}}},
			want:  "InternalError",
		},
		{
			name:  "partial final page",
			pages: []*cloudwatch.GetMetricDataOutput{{MetricDataResults: []cwtypes.MetricDataResult{{Id: aws.String("m0"), StatusCode: cwtypes.StatusCodePartialData}}}},
			want:  "PartialData",
		},
		{
			name:  "missing metric",
			pages: []*cloudwatch.GetMetricDataOutput{{MetricDataResults: []cwtypes.MetricDataResult{complete("m0")}}},
			want:  "MetricB",
		},
		{
			name:  "missing result ID",
			pages: []*cloudwatch.GetMetricDataOutput{{MetricDataResults: []cwtypes.MetricDataResult{{StatusCode: cwtypes.StatusCodeComplete}}}},
			want:  "without an ID",
		},
		{
			name:  "unexpected result ID",
			pages: []*cloudwatch.GetMetricDataOutput{{MetricDataResults: []cwtypes.MetricDataResult{complete("unexpected")}}},
			want:  "unexpected",
		},
		{
			name: "duplicate pagination token",
			pages: []*cloudwatch.GetMetricDataOutput{
				{MetricDataResults: []cwtypes.MetricDataResult{{Id: aws.String("m0"), StatusCode: cwtypes.StatusCodePartialData}, {Id: aws.String("m1"), StatusCode: cwtypes.StatusCodePartialData}}, NextToken: aws.String("same")},
				{MetricDataResults: []cwtypes.MetricDataResult{{Id: aws.String("m0"), StatusCode: cwtypes.StatusCodePartialData}, {Id: aws.String("m1"), StatusCode: cwtypes.StatusCodePartialData}}, NextToken: aws.String("same")},
			},
			want: "duplicate pagination token",
		},
		{
			name: "never completes",
			pages: []*cloudwatch.GetMetricDataOutput{
				{MetricDataResults: []cwtypes.MetricDataResult{{Id: aws.String("m0"), StatusCode: cwtypes.StatusCodePartialData}, complete("m1")}, NextToken: aws.String("page-2")},
				{MetricDataResults: []cwtypes.MetricDataResult{complete("m1")}},
			},
			want: "MetricA",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeCloudwatchMetricDataClient{pages: test.pages}
			_, err := cloudwatchGetMetricData(
				context.Background(), client, 60, "Sum", from, to,
				"Test/Namespace", []string{"MetricA", "MetricB"}, "Name=value",
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCloudwatchGetMetricDataPropagatesClientError(t *testing.T) {
	from, to := cloudwatchMetricDataTestRange()
	client := &fakeCloudwatchMetricDataClient{err: errors.New("network unavailable")}
	_, err := cloudwatchGetMetricData(
		context.Background(), client, 60, "Sum", from, to,
		"Test/Namespace", []string{"Metric"}, "Name=value",
	)
	if err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("error = %v", err)
	}
}

var _ cloudwatchMetricDataClient = (*fakeCloudwatchMetricDataClient)(nil)

func TestCloudwatchWriteMetricDataPreservesSparseSeries(t *testing.T) {
	older := time.Date(2026, time.August, 28, 22, 41, 0, 0, time.UTC)
	newer := older.Add(time.Minute)
	newest := newer.Add(time.Minute)
	results := []cwtypes.MetricDataResult{
		{
			Id:    aws.String("m3"),
			Label: aws.String("human label D"),
		},
		{
			Id:         aws.String("m2"),
			Label:      aws.String("human label C"),
			Timestamps: []time.Time{newer, older},
			Values:     []float64{0, 0},
		},
		{
			Id:         aws.String("m1"),
			Label:      aws.String("human label B"),
			Timestamps: []time.Time{newest, older},
			Values:     []float64{9, 0},
		},
		{
			Id:         aws.String("m0"),
			Label:      aws.String("human label A"),
			Timestamps: []time.Time{newer, older},
			Values:     []float64{2, 1},
		},
	}

	var output bytes.Buffer
	if err := CloudwatchWriteMetricData(
		&output,
		[]string{"Invocations", "Errors", "Throttles", "Empty"},
		"FunctionName=better-beta",
		results,
	); err != nil {
		t.Fatal(err)
	}
	want := "timestamp=2026-08-28T22:43:00Z " +
		"FunctionName-better-beta::Errors=9 \n" +
		"timestamp=2026-08-28T22:42:00Z " +
		"FunctionName-better-beta::Invocations=2 " +
		"FunctionName-better-beta::Throttles=0 \n" +
		"timestamp=2026-08-28T22:41:00Z " +
		"FunctionName-better-beta::Invocations=1 " +
		"FunctionName-better-beta::Errors=0 " +
		"FunctionName-better-beta::Throttles=0 \n"
	if output.String() != want {
		t.Fatalf("sparse metric output mismatch:\n got: %q\nwant: %q", output.String(), want)
	}
}

func TestCloudwatchGetMetricSparseIntegration(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// CloudWatch has no metric deletion API, so keep one fixed test identity across runs.
	const namespace = "Libaws/IntegrationTests"
	const dimensionName = "Suite"
	const dimensionValue = "CloudwatchGetMetricSparse"
	older := time.Now().UTC().Add(-30 * time.Second).Truncate(time.Second)
	newer := older.Add(time.Second)
	dimension := cwtypes.Dimension{Name: aws.String(dimensionName), Value: aws.String(dimensionValue)}
	_, err := CloudwatchClient().PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace: aws.String(namespace),
		MetricData: []cwtypes.MetricDatum{
			{MetricName: aws.String("Dense"), Dimensions: []cwtypes.Dimension{dimension}, Timestamp: aws.Time(older), Value: aws.Float64(11), StorageResolution: aws.Int32(1)},
			{MetricName: aws.String("Dense"), Dimensions: []cwtypes.Dimension{dimension}, Timestamp: aws.Time(newer), Value: aws.Float64(17), StorageResolution: aws.Int32(1)},
			{MetricName: aws.String("Sparse"), Dimensions: []cwtypes.Dimension{dimension}, Timestamp: aws.Time(older), Value: aws.Float64(13), StorageResolution: aws.Int32(1)},
		},
	})
	if err != nil {
		t.Fatalf("put sparse CloudWatch metrics: %v", err)
	}

	from := older.Add(-time.Second)
	to := newer.Add(2 * time.Second)
	var lastOutput string
	for {
		results, getErr := CloudwatchGetMetricData(
			ctx, 1, "Sum", &from, &to, namespace,
			[]string{"Dense", "Sparse"}, dimensionName+"="+dimensionValue,
		)
		if getErr == nil {
			var output bytes.Buffer
			getErr = CloudwatchWriteMetricData(
				&output,
				[]string{"Dense", "Sparse"},
				dimensionName+"="+dimensionValue,
				results,
			)
			lastOutput = output.String()
			if getErr == nil {
				olderLine := cloudwatchMetricLineAt(lastOutput, older)
				newerLine := cloudwatchMetricLineAt(lastOutput, newer)
				if strings.Contains(olderLine, "::Dense=11 ") &&
					strings.Contains(olderLine, "::Sparse=13 ") &&
					strings.Contains(newerLine, "::Dense=17 ") &&
					!strings.Contains(newerLine, "::Sparse=") {
					t.Logf("sparse CloudWatch output:\n%s", lastOutput)
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("sparse CloudWatch metrics did not converge: %v; last output:\n%s", getErr, lastOutput)
		case <-time.After(5 * time.Second):
		}
	}
}

func cloudwatchMetricLineAt(output string, timestamp time.Time) string {
	prefix := "timestamp=" + timestamp.UTC().Format(time.RFC3339Nano) + " "
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
