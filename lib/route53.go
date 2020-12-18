package lib

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go/service/route53"
)

var r53Client *route53.Route53
var r53ClientLock sync.RWMutex

func Route53Client() *route53.Route53 {
	r53ClientLock.Lock()
	defer r53ClientLock.Unlock()
	if r53Client == nil {
		r53Client = route53.New(Session())
	}
	return r53Client
}

func Route53ListRecords(ctx context.Context, zoneId *string) <-chan *route53.ResourceRecordSet {
	var nextId *string
	var nextName *string
	var nextType *string
	records := make(chan *route53.ResourceRecordSet)
	go func() {
		out, err := Route53Client().ListResourceRecordSetsWithContext(ctx, &route53.ListResourceRecordSetsInput{
			HostedZoneId:          zoneId,
			StartRecordIdentifier: nextId,
			StartRecordName:       nextName,
			StartRecordType:       nextType,
		})
		Panic1(err)
		for _, record := range out.ResourceRecordSets {
			select {
			case <-ctx.Done():
				close(records)
				return
			case records <- record:
			}
		}
		if !*out.IsTruncated {
			close(records)
			return
		}
		nextId = out.NextRecordIdentifier
		nextName = out.NextRecordName
		nextType = out.NextRecordType
	}()
	return records
}

func Route53ListZones(ctx context.Context) <-chan *route53.HostedZone {
	var nextDns *string
	var nextId *string
	zones := make(chan *route53.HostedZone)
	go func() {
		for {
			out, err := Route53Client().ListHostedZonesByNameWithContext(ctx, &route53.ListHostedZonesByNameInput{
				DNSName:      nextDns,
				HostedZoneId: nextId,
			})
			Panic1(err)
			for _, zone := range out.HostedZones {
				zones <- zone
			}
			if !*out.IsTruncated {
				close(zones)
				return
			}
			nextDns = out.NextDNSName
			nextId = out.NextHostedZoneId
		}
	}()
	return zones
}
