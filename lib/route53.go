package lib

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
)

var r53Client *route53.Client
var r53ClientLock sync.RWMutex

func Route53Client() *route53.Client {
	r53ClientLock.Lock()
	defer r53ClientLock.Unlock()
	if r53Client == nil {
		r53Client = route53.NewFromConfig(Config())
	}
	return r53Client
}

func Route53ListRecords(ctx context.Context, zoneId *string) <-chan types.ResourceRecordSet {
	var nextId *string
	var nextName *string
	var nextType types.RRType
	records := make(chan types.ResourceRecordSet)
	go func() {
		out, err := Route53Client().ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
			HostedZoneId:          zoneId,
			StartRecordIdentifier: nextId,
			StartRecordName:       nextName,
			StartRecordType:       nextType,
		})
		panic1(err)
		for _, record := range out.ResourceRecordSets {
			select {
			case <-ctx.Done():
				close(records)
				return
			case records <- record:
			}

		}
		if !out.IsTruncated {
			close(records)
			return
		}
		nextId = out.NextRecordIdentifier
		nextName = out.NextRecordName
		nextType = out.NextRecordType
	}()
	return records
}

func Route53ListZones() <-chan types.HostedZone {
	var nextDns *string
	var nextId *string
	zones := make(chan types.HostedZone)
	go func() {
		for {
			out, err := Route53Client().ListHostedZonesByName(ctx, &route53.ListHostedZonesByNameInput{
				DNSName:      nextDns,
				HostedZoneId: nextId,
			})
			panic1(err)
			for _, zone := range out.HostedZones {
				zones <- zone
			}
			if !out.IsTruncated {
				close(zones)
				return
			}
			nextDns = out.NextDNSName
			nextId = out.NextHostedZoneId
		}
	}()
	return zones
}
