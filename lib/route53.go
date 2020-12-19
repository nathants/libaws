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

func Route53ListRecords(ctx context.Context, zoneId *string) ([]*route53.ResourceRecordSet, error) {
	var nextId *string
	var nextName *string
	var nextType *string
	var records []*route53.ResourceRecordSet
	for {
		out, err := Route53Client().ListResourceRecordSetsWithContext(ctx, &route53.ListResourceRecordSetsInput{
			HostedZoneId:          zoneId,
			StartRecordIdentifier: nextId,
			StartRecordName:       nextName,
			StartRecordType:       nextType,
		})
		if err != nil {
			return nil, err
		}
		records = append(records, out.ResourceRecordSets...)
		if !*out.IsTruncated {
			break
		}
		nextId = out.NextRecordIdentifier
		nextName = out.NextRecordName
		nextType = out.NextRecordType
	}
	return records, nil
}

func Route53ListZones(ctx context.Context) ([]*route53.HostedZone, error) {
	var nextDns *string
	var nextId *string
	var zones []*route53.HostedZone
	for {
		out, err := Route53Client().ListHostedZonesByNameWithContext(ctx, &route53.ListHostedZonesByNameInput{
			DNSName:      nextDns,
			HostedZoneId: nextId,
		})
		if err != nil {
			return nil, err
		}
		zones = append(zones, out.HostedZones...)
		if !*out.IsTruncated {
			break
		}
		nextDns = out.NextDNSName
		nextId = out.NextHostedZoneId
	}
	return zones, nil
}
