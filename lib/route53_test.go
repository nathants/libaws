package lib

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

func TestRoute53DNSNameEqualityPreservesLabelBoundaries(t *testing.T) {
	if !route53DNSNameEqual(`\052.example.com.`, `*.EXAMPLE.com`) {
		t.Fatal("AWS-escaped wildcard did not match its literal DNS name")
	}
	if route53DNSNameEqual(`api\056internal.example.com.`, `api.internal.example.com`) {
		t.Fatal("escaped data octet matched a DNS label separator")
	}
	if got := route53DNSName(`api\012.example.com.`); got != `api\012.example.com` {
		t.Fatalf("display name decoded control data: %q", got)
	}
}

func route53TestSimpleRecord() r53types.ResourceRecordSet {
	return r53types.ResourceRecordSet{
		Name:            aws.String("api.example.com."),
		Type:            r53types.RRTypeA,
		TTL:             aws.Int64(60),
		ResourceRecords: []r53types.ResourceRecord{{Value: aws.String("192.0.2.1")}},
	}
}

func TestRoute53RecordIsSimpleRejectsEveryAdvancedRoutingField(t *testing.T) {
	mutations := map[string]func(*r53types.ResourceRecordSet){
		"cidr": func(record *r53types.ResourceRecordSet) {
			record.CidrRoutingConfig = &r53types.CidrRoutingConfig{}
		},
		"failover": func(record *r53types.ResourceRecordSet) {
			record.Failover = r53types.ResourceRecordSetFailoverPrimary
		},
		"geolocation": func(record *r53types.ResourceRecordSet) {
			record.GeoLocation = &r53types.GeoLocation{}
		},
		"geoproximity": func(record *r53types.ResourceRecordSet) {
			record.GeoProximityLocation = &r53types.GeoProximityLocation{}
		},
		"health check": func(record *r53types.ResourceRecordSet) {
			record.HealthCheckId = aws.String("health-check")
		},
		"multivalue": func(record *r53types.ResourceRecordSet) {
			record.MultiValueAnswer = aws.Bool(true)
		},
		"latency region": func(record *r53types.ResourceRecordSet) {
			record.Region = r53types.ResourceRecordSetRegionUsEast1
		},
		"set identifier": func(record *r53types.ResourceRecordSet) {
			record.SetIdentifier = aws.String("set")
		},
		"traffic policy": func(record *r53types.ResourceRecordSet) {
			record.TrafficPolicyInstanceId = aws.String("policy")
		},
		"weight": func(record *r53types.ResourceRecordSet) {
			record.Weight = aws.Int64(1)
		},
	}
	if record := route53TestSimpleRecord(); !route53RecordIsSimple(&record) {
		t.Fatal("ordinary record was not simple")
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			record := route53TestSimpleRecord()
			mutate(&record)
			if route53RecordIsSimple(&record) {
				t.Fatalf("advanced record was considered simple: %#v", record)
			}
			desired := route53TestSimpleRecord()
			if _, err := route53SimpleRecord([]r53types.ResourceRecordSet{record}, &desired); err == nil ||
				!strings.Contains(err.Error(), "unsupported advanced routing") {
				t.Fatalf("simple mutation error = %v", err)
			}
		})
	}
}

func TestRoute53SimpleRecordRejectsMultipleSameNameAndTypeRecords(t *testing.T) {
	desired := route53TestSimpleRecord()
	other := route53TestSimpleRecord()
	other.ResourceRecords = []r53types.ResourceRecord{{Value: aws.String("192.0.2.2")}}
	if _, err := route53SimpleRecord([]r53types.ResourceRecordSet{desired, other}, &desired); err == nil ||
		!strings.Contains(err.Error(), "multiple Route53 records") {
		t.Fatalf("multiple-record error = %v", err)
	}
}

type fakeRoute53RecordListClient struct {
	outputs []*route53.ListResourceRecordSetsOutput
	calls   int
}

func (client *fakeRoute53RecordListClient) ListResourceRecordSets(
	context.Context,
	*route53.ListResourceRecordSetsInput,
	...func(*route53.Options),
) (*route53.ListResourceRecordSetsOutput, error) {
	output := client.outputs[client.calls]
	client.calls++
	return output, nil
}

func TestRoute53ListRecordsRejectsPaginationCycles(t *testing.T) {
	page := func(cursor string) *route53.ListResourceRecordSetsOutput {
		return &route53.ListResourceRecordSetsOutput{
			IsTruncated:    true,
			NextRecordName: aws.String(cursor + ".example.com"),
			NextRecordType: r53types.RRTypeA,
		}
	}
	client := &fakeRoute53RecordListClient{outputs: []*route53.ListResourceRecordSetsOutput{
		page("first"), page("second"), page("first"),
	}}
	if _, err := route53ListRecords(context.Background(), client, "zone"); err == nil ||
		!strings.Contains(err.Error(), "repeated record pagination cursor") {
		t.Fatalf("pagination error = %v", err)
	}
	if client.calls != 3 {
		t.Fatalf("list calls = %d, want 3", client.calls)
	}
}
