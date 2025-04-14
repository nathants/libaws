package lib

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/gofrs/uuid"
)

var r53Client *route53.Client
var r53ClientLock sync.Mutex

func Route53ClientExplicit(accessKeyID, accessKeySecret, region string) *route53.Client {
	return route53.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func Route53Client() *route53.Client {
	r53ClientLock.Lock()
	defer r53ClientLock.Unlock()
	if r53Client == nil {
		r53Client = route53.NewFromConfig(*Session())
	}
	return r53Client
}

func Route53DeleteRecord(ctx context.Context, input *route53EnsureRecordInput, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Route53DeleteRecord"}
		defer d.Log()
	}
	id, err := Route53ZoneID(ctx, input.zoneName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	records, err := Route53ListRecords(ctx, id)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	var record *r53types.ResourceRecordSet
	for _, r := range records {
		if strings.TrimRight(*r.Name, ".") != *input.change.ResourceRecordSet.Name {
			continue
		}
		if input.change.ResourceRecordSet.TTL != nil && *r.TTL != *input.change.ResourceRecordSet.TTL {
			continue
		}
		if r.Type != input.change.ResourceRecordSet.Type {
			continue
		}
		if r.AliasTarget != nil && input.change.ResourceRecordSet.AliasTarget != nil {
			if !reflect.DeepEqual(r.AliasTarget, input.change.ResourceRecordSet.AliasTarget) {
				continue
			}
		} else {
			if !reflect.DeepEqual(r.ResourceRecords, input.change.ResourceRecordSet.ResourceRecords) {
				continue
			}
		}
		record = &r
	}
	if record != nil {
		if !preview {
			_, err = Route53Client().ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
				HostedZoneId: aws.String(id),
				ChangeBatch: &r53types.ChangeBatch{
					Changes: []r53types.Change{{
						Action:            r53types.ChangeActionDelete,
						ResourceRecordSet: record,
					}},
				},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		if input.change.ResourceRecordSet.AliasTarget == nil {
			var vals []string
			for _, r := range input.change.ResourceRecordSet.ResourceRecords {
				vals = append(vals, "Value="+*r.Value)
			}
			Logger.Printf(PreviewString(preview)+"route53 deleted record %s: %s %s %s\n",
				strings.TrimRight(*input.change.ResourceRecordSet.Name, "."),
				"TTL="+fmt.Sprint(*input.change.ResourceRecordSet.TTL),
				"Type="+string(input.change.ResourceRecordSet.Type),
				strings.Join(vals, " "),
			)
		} else {
			Logger.Printf(PreviewString(preview)+"route53 deleted record %s: %s %s %s\n",
				strings.TrimRight(*input.change.ResourceRecordSet.Name, "."),
				"Type=Alias",
				"Value="+*input.change.ResourceRecordSet.AliasTarget.DNSName,
				"HostedZoneId="+*input.change.ResourceRecordSet.AliasTarget.HostedZoneId,
			)
		}
	}
	return nil
}

func Route53DeleteZone(ctx context.Context, name string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Route53DeleteZone"}
		defer d.Log()
	}
	id, err := Route53ZoneID(ctx, name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if !preview {
		_, err := Route53Client().DeleteHostedZone(ctx, &route53.DeleteHostedZoneInput{
			Id: aws.String(id),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"route53 deleted hosted name:", name, id)
	return nil
}

type route53EnsureRecordInput struct {
	zoneName string
	change   *r53types.Change
}

func Route53EnsureRecordInput(zoneName, recordName string, attrs []string) (*route53EnsureRecordInput, error) {
	zoneName = strings.Trim(zoneName, ".")
	recordName = strings.Trim(recordName, ".")
	input := &route53EnsureRecordInput{
		zoneName: zoneName,
		change: &r53types.Change{
			Action:            r53types.ChangeActionUpsert,
			ResourceRecordSet: &r53types.ResourceRecordSet{},
		},
	}
	if !strings.HasSuffix(recordName, zoneName) {
		err := fmt.Errorf("record-name must have suffix of zone-name: %s %s", recordName, zoneName)
		Logger.Println("error:", err)
		return nil, err
	}
	input.change.ResourceRecordSet.Name = aws.String(recordName)
	for _, attr := range attrs {
		head, value, err := SplitOnce(attr, "=")
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		head = strings.ToLower(head)
		var tail string
		if strings.Contains(head, ".") {
			var err error
			head, tail, err = SplitOnce(head, ".")
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
		}
		_ = tail
		switch head {
		case "ttl":
			ttl, err := strconv.Atoi(value)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			input.change.ResourceRecordSet.TTL = aws.Int64(int64(ttl))
		case "type":
			if value == "Alias" {
				input.change.ResourceRecordSet.Type = r53types.RRTypeA
				input.change.ResourceRecordSet.AliasTarget = &r53types.AliasTarget{
					EvaluateTargetHealth: false,
				}
			} else {
				var rrtype r53types.RRType
				if !slices.Contains(rrtype.Values(), r53types.RRType(value)) {
					err := fmt.Errorf("route53 unknown type: %s", attr)
					Logger.Println("error:", err)
					return nil, err
				}
				input.change.ResourceRecordSet.Type = r53types.RRType(value)
			}
		case "value":
			if input.change.ResourceRecordSet.AliasTarget == nil {
				input.change.ResourceRecordSet.ResourceRecords = append(
					input.change.ResourceRecordSet.ResourceRecords,
					r53types.ResourceRecord{Value: aws.String(value)},
				)
			} else {
				input.change.ResourceRecordSet.AliasTarget.DNSName = aws.String(value)
			}
		case "hostedzoneid":
			input.change.ResourceRecordSet.AliasTarget.HostedZoneId = aws.String(value)
		default:
			err := fmt.Errorf("route53 unknown record attr: %s", attr)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return input, nil
}

func Route53ZoneID(ctx context.Context, name string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Route53ZoneID"}
		defer d.Log()
	}
	var id string
	zones, err := Route53ListZones(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	count := 0
	for _, zone := range zones {
		if name == strings.TrimRight(*zone.Name, ".") {
			id = *zone.Id
			count++
		}
	}
	switch count {
	case 0:
		err := fmt.Errorf("route53 zone not found with name: %s", name)
		Logger.Println("error:", err)
		return "", err
	case 1:
		return id, nil
	default:
		err := fmt.Errorf("route53 found more than one hosted zone with name: %s", name)
		Logger.Println("error:", err)
		return "", err
	}
}

func Route53EnsureRecord(ctx context.Context, input *route53EnsureRecordInput, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Route53EnsureRecord"}
		defer d.Log()
	}
	id, err := Route53ZoneID(ctx, input.zoneName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	records, err := Route53ListRecords(ctx, id)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	// default to true to add records that don't exist
	needsUpdate := false
	exists := false
	for _, record := range records {
		// only update records when name matches
		if strings.TrimRight(*record.Name, ".") != *input.change.ResourceRecordSet.Name {
			continue
		}
		// only update records when type matches
		if record.Type != input.change.ResourceRecordSet.Type {
			continue
		}
		// found the record, assume it's already correct until we find a value that isn't
		exists = true
		if record.AliasTarget != nil && input.change.ResourceRecordSet.AliasTarget != nil {
			if !reflect.DeepEqual(record.AliasTarget.DNSName, input.change.ResourceRecordSet.AliasTarget.DNSName) {
				Logger.Printf(PreviewString(preview)+"route53 update Alias for %s: %v => %v\n",
					strings.TrimRight(*record.Name, "."),
					*record.AliasTarget.DNSName,
					*input.change.ResourceRecordSet.AliasTarget.DNSName,
				)
				needsUpdate = true
			}
			if !reflect.DeepEqual(record.AliasTarget.HostedZoneId, input.change.ResourceRecordSet.AliasTarget.HostedZoneId) {
				Logger.Printf(PreviewString(preview)+"route53 update HostedZoneId for %s: %v => %v\n",
					strings.TrimRight(*record.Name, "."),
					*record.AliasTarget.HostedZoneId,
					*input.change.ResourceRecordSet.AliasTarget.HostedZoneId,
				)
				needsUpdate = true
			}
		} else {
			if !reflect.DeepEqual(record.TTL, input.change.ResourceRecordSet.TTL) {
				Logger.Printf(PreviewString(preview)+"route53 update TTL for %s: %d => %d\n",
					strings.TrimRight(*record.Name, "."),
					*record.TTL,
					*input.change.ResourceRecordSet.TTL,
				)
				needsUpdate = true
			}
			if !reflect.DeepEqual(record.ResourceRecords, input.change.ResourceRecordSet.ResourceRecords) {
				var old []string
				for _, r := range record.ResourceRecords {
					old = append(old, *r.Value)
				}
				var new []string
				for _, r := range input.change.ResourceRecordSet.ResourceRecords {
					new = append(new, *r.Value)
				}
				Logger.Printf(PreviewString(preview)+"route53 update Values for %s: %s => %s\n",
					strings.TrimRight(*record.Name, "."),
					Json(old),
					Json(new),
				)
				needsUpdate = true
			}
		}
	}
	if needsUpdate || !exists {
		if !needsUpdate {
			if input.change.ResourceRecordSet.AliasTarget == nil {
				var vals []string
				for _, r := range input.change.ResourceRecordSet.ResourceRecords {
					vals = append(vals, "Value="+*r.Value)
				}
				Logger.Printf(PreviewString(preview)+"route53 create record %s: %s %s %s\n",
					strings.TrimRight(*input.change.ResourceRecordSet.Name, "."),
					"TTL="+fmt.Sprint(*input.change.ResourceRecordSet.TTL),
					"Type="+string(input.change.ResourceRecordSet.Type),
					strings.Join(vals, " "),
				)
			} else {
				Logger.Printf(PreviewString(preview)+"route53 create record %s: %s %s %s\n",
					strings.TrimRight(*input.change.ResourceRecordSet.Name, "."),
					"Type=Alias",
					"Value="+*input.change.ResourceRecordSet.AliasTarget.DNSName,
					"HostedZoneId="+*input.change.ResourceRecordSet.AliasTarget.HostedZoneId,
				)
			}
		}
		if !preview {
			_, err = Route53Client().ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
				HostedZoneId: aws.String(id),
				ChangeBatch: &r53types.ChangeBatch{
					Changes: []r53types.Change{*input.change},
				},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			Logger.Println("route53 updated record: " + *input.change.ResourceRecordSet.Name)
		}
	}
	return nil
}

func Route53EnsureZone(ctx context.Context, name string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Route53EnsureZone"}
		defer d.Log()
	}
	name = strings.Trim(name, ".")
	zones, err := Route53ListZones(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	count := 0
	for _, zone := range zones {
		if name == strings.TrimRight(*zone.Name, ".") {
			count++
		}
	}
	switch count {
	case 0:
	case 1:
		return nil
	default:
		err := fmt.Errorf("route53 found more than one hosted zone with name: %s", name)
		Logger.Println("error:", err)
		return err
	}
	if !preview {
		_, err = Route53Client().CreateHostedZone(ctx, &route53.CreateHostedZoneInput{
			Name:            aws.String(name),
			CallerReference: aws.String(uuid.Must(uuid.NewV4()).String()),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	outer:
		for {
			zones, err := Route53ListZones(ctx)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, zone := range zones {
				if name == strings.TrimRight(*zone.Name, ".") {
					break outer
				}
			}
			time.Sleep(time.Second * 5)
			Logger.Println("route53 wait for zone to be created:", name)
		}
	}
	Logger.Println(PreviewString(preview)+"route53 created zone:", name)
	return nil
}

func Route53ListRecords(ctx context.Context, zoneId string) ([]r53types.ResourceRecordSet, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Route53ListRecords"}
		defer d.Log()
	}
	var nextId *string
	var nextName *string
	var nextType r53types.RRType
	var records []r53types.ResourceRecordSet
	for {
		out, err := Route53Client().ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
			HostedZoneId:          aws.String(zoneId),
			StartRecordIdentifier: nextId,
			StartRecordName:       nextName,
			StartRecordType:       nextType,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		records = append(records, out.ResourceRecordSets...)
		if out.NextRecordIdentifier == nil && out.NextRecordName == nil && out.NextRecordType == "" {
			break
		}
		nextId = out.NextRecordIdentifier
		nextName = out.NextRecordName
		nextType = out.NextRecordType
	}
	return records, nil
}

func Route53ListZones(ctx context.Context) ([]r53types.HostedZone, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Route53ListZones"}
		defer d.Log()
	}
	var nextDns *string
	var nextId *string
	var zones []r53types.HostedZone
	for {
		out, err := Route53Client().ListHostedZonesByName(ctx, &route53.ListHostedZonesByNameInput{
			DNSName:      nextDns,
			HostedZoneId: nextId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		zones = append(zones, out.HostedZones...)
		if out.NextDNSName == nil && out.NextHostedZoneId == nil {
			break
		}
		nextDns = out.NextDNSName
		nextId = out.NextHostedZoneId
	}
	return zones, nil
}
