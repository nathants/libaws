package lib

import (
	"context"
	"errors"
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

func route53DNSName(name string) string {
	return strings.ReplaceAll(strings.TrimSuffix(name, "."), `\052`, "*")
}

// Route53 escapes label data as \DDD, so encode bytes while keeping label-separator dots distinct.
func route53DNSNameKey(name string) string {
	name = strings.TrimSuffix(name, ".")
	var normalized strings.Builder
	normalized.Grow(len(name) * 4)
	for index := 0; index < len(name); {
		value := name[index]
		escaped := false
		if index+3 < len(name) && value == '\\' &&
			name[index+1] >= '0' && name[index+1] <= '3' &&
			name[index+2] >= '0' && name[index+2] <= '7' &&
			name[index+3] >= '0' && name[index+3] <= '7' {
			value = (name[index+1]-'0')*64 + (name[index+2]-'0')*8 + name[index+3] - '0'
			index += 4
			escaped = true
		} else {
			index++
		}
		if value == '.' && !escaped {
			normalized.WriteByte('.')
			continue
		}
		if value >= 'A' && value <= 'Z' {
			value += 'a' - 'A'
		}
		normalized.WriteByte('\\')
		normalized.WriteByte('0' + value/64)
		normalized.WriteByte('0' + value/8%8)
		normalized.WriteByte('0' + value%8)
	}
	return normalized.String()
}

func route53DNSNameEqual(left, right string) bool {
	return route53DNSNameKey(left) == route53DNSNameKey(right)
}

func route53RecordIsSimple(record *r53types.ResourceRecordSet) bool {
	if record == nil || aws.ToString(record.Name) == "" || record.Type == "" ||
		record.CidrRoutingConfig != nil || record.Failover != "" || record.GeoLocation != nil ||
		record.GeoProximityLocation != nil || record.HealthCheckId != nil ||
		(record.MultiValueAnswer != nil && aws.ToBool(record.MultiValueAnswer)) || record.Region != "" ||
		record.SetIdentifier != nil || record.TrafficPolicyInstanceId != nil || record.Weight != nil {
		return false
	}
	if record.AliasTarget != nil {
		return aws.ToString(record.AliasTarget.DNSName) != "" &&
			aws.ToString(record.AliasTarget.HostedZoneId) != "" && record.TTL == nil &&
			len(record.ResourceRecords) == 0
	}
	if record.TTL == nil || len(record.ResourceRecords) == 0 {
		return false
	}
	for _, value := range record.ResourceRecords {
		if value.Value == nil {
			return false
		}
	}
	return true
}

func route53SimpleRecord(
	records []r53types.ResourceRecordSet,
	desired *r53types.ResourceRecordSet,
) (*r53types.ResourceRecordSet, error) {
	if desired == nil || aws.ToString(desired.Name) == "" || desired.Type == "" {
		return nil, errors.New("cannot match a Route53 record without a name and type")
	}
	var matches []int
	for index := range records {
		if route53DNSNameEqual(aws.ToString(records[index].Name), aws.ToString(desired.Name)) &&
			records[index].Type == desired.Type {
			matches = append(matches, index)
		}
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple Route53 records match %s %s; advanced routing is unsupported",
			route53DNSName(aws.ToString(desired.Name)), desired.Type)
	}
	if len(matches) == 0 {
		return nil, nil
	}
	record := &records[matches[0]]
	if !route53RecordIsSimple(record) {
		return nil, fmt.Errorf("Route53 record %s %s uses unsupported advanced routing",
			route53DNSName(aws.ToString(desired.Name)), desired.Type)
	}
	return record, nil
}

func Route53DeleteRecord(ctx context.Context, input *route53EnsureRecordInput, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Route53DeleteRecord"}
		d.Start()
		defer d.End()
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
	desired := input.change.ResourceRecordSet
	if _, err := route53SimpleRecord(records, desired); err != nil {
		Logger.Println("error:", err)
		return err
	}
	var record *r53types.ResourceRecordSet
	for _, current := range records {
		if !route53DNSNameEqual(aws.ToString(current.Name), aws.ToString(desired.Name)) ||
			current.Type != desired.Type || (current.AliasTarget == nil) != (desired.AliasTarget == nil) {
			continue
		}
		if desired.AliasTarget != nil {
			if !route53DNSNameEqual(
				aws.ToString(current.AliasTarget.DNSName), aws.ToString(desired.AliasTarget.DNSName),
			) || aws.ToString(current.AliasTarget.HostedZoneId) != aws.ToString(desired.AliasTarget.HostedZoneId) ||
				current.AliasTarget.EvaluateTargetHealth != desired.AliasTarget.EvaluateTargetHealth {
				continue
			}
		} else if current.TTL == nil || desired.TTL == nil || *current.TTL != *desired.TTL ||
			!reflect.DeepEqual(current.ResourceRecords, desired.ResourceRecords) {
			continue
		}
		record = &current
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
		d.Start()
		defer d.End()
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
		d.Start()
		defer d.End()
	}
	var id string
	zones, err := Route53ListZones(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	count := 0
	for _, zone := range zones {
		if route53DNSNameEqual(name, aws.ToString(zone.Name)) {
			id = aws.ToString(zone.Id)
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
		d.Start()
		defer d.End()
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
	// Add the record when no matching name and type exists.
	needsUpdate := false
	exists := false
	desired := input.change.ResourceRecordSet
	if _, err := route53SimpleRecord(records, desired); err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, record := range records {
		// only update records when name matches
		if !route53DNSNameEqual(aws.ToString(record.Name), aws.ToString(desired.Name)) {
			continue
		}
		// only update records when type matches
		if record.Type != desired.Type {
			continue
		}
		// found the record, assume it's already correct until we find a value that isn't
		exists = true
		if (record.AliasTarget == nil) != (desired.AliasTarget == nil) {
			Logger.Printf(PreviewString(preview)+"route53 update record shape for %s\n", route53DNSName(aws.ToString(record.Name)))
			needsUpdate = true
			continue
		}
		if desired.AliasTarget != nil {
			if !route53DNSNameEqual(aws.ToString(record.AliasTarget.DNSName), aws.ToString(desired.AliasTarget.DNSName)) {
				Logger.Printf(PreviewString(preview)+"route53 update Alias for %s: %v => %v\n",
					route53DNSName(aws.ToString(record.Name)),
					aws.ToString(record.AliasTarget.DNSName),
					aws.ToString(desired.AliasTarget.DNSName),
				)
				needsUpdate = true
			}
			if aws.ToString(record.AliasTarget.HostedZoneId) != aws.ToString(desired.AliasTarget.HostedZoneId) {
				Logger.Printf(PreviewString(preview)+"route53 update HostedZoneId for %s: %v => %v\n",
					route53DNSName(aws.ToString(record.Name)),
					aws.ToString(record.AliasTarget.HostedZoneId),
					aws.ToString(desired.AliasTarget.HostedZoneId),
				)
				needsUpdate = true
			}
			if record.AliasTarget.EvaluateTargetHealth != desired.AliasTarget.EvaluateTargetHealth {
				Logger.Printf(PreviewString(preview)+"route53 update EvaluateTargetHealth for %s: %v => %v\n",
					route53DNSName(aws.ToString(record.Name)),
					record.AliasTarget.EvaluateTargetHealth,
					desired.AliasTarget.EvaluateTargetHealth,
				)
				needsUpdate = true
			}
		} else {
			if record.TTL == nil || desired.TTL == nil || *record.TTL != *desired.TTL {
				Logger.Printf(PreviewString(preview)+"route53 update TTL for %s: %d => %d\n",
					route53DNSName(aws.ToString(record.Name)),
					aws.ToInt64(record.TTL),
					aws.ToInt64(desired.TTL),
				)
				needsUpdate = true
			}
			if !reflect.DeepEqual(record.ResourceRecords, desired.ResourceRecords) {
				var old []string
				for _, r := range record.ResourceRecords {
					old = append(old, aws.ToString(r.Value))
				}
				var new []string
				for _, r := range desired.ResourceRecords {
					new = append(new, aws.ToString(r.Value))
				}
				Logger.Printf(PreviewString(preview)+"route53 update Values for %s: %s => %s\n",
					route53DNSName(aws.ToString(record.Name)),
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
		d.Start()
		defer d.End()
	}
	name = strings.Trim(name, ".")
	zones, err := Route53ListZones(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	count := 0
	for _, zone := range zones {
		if route53DNSNameEqual(name, aws.ToString(zone.Name)) {
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
				if route53DNSNameEqual(name, aws.ToString(zone.Name)) {
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

type route53RecordListClient interface {
	ListResourceRecordSets(context.Context, *route53.ListResourceRecordSetsInput, ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error)
}

func route53ListRecords(ctx context.Context, client route53RecordListClient, zoneID string) ([]r53types.ResourceRecordSet, error) {
	var records []r53types.ResourceRecordSet
	var nextID *string
	var nextName *string
	var nextType r53types.RRType
	type cursor struct {
		identifier string
		name       string
		recordType r53types.RRType
	}
	seenCursors := map[cursor]bool{}
	for {
		out, err := client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
			HostedZoneId:          aws.String(zoneID),
			StartRecordIdentifier: nextID,
			StartRecordName:       nextName,
			StartRecordType:       nextType,
		})
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, errors.New("Route53 ListResourceRecordSets returned nil output")
		}
		records = append(records, out.ResourceRecordSets...)
		if !out.IsTruncated {
			return records, nil
		}
		if out.NextRecordName == nil || out.NextRecordType == "" {
			return nil, errors.New("Route53 returned a truncated record page without a complete cursor")
		}
		nextCursor := cursor{
			identifier: aws.ToString(out.NextRecordIdentifier),
			name:       aws.ToString(out.NextRecordName),
			recordType: out.NextRecordType,
		}
		if seenCursors[nextCursor] {
			return nil, errors.New("Route53 returned a repeated record pagination cursor")
		}
		seenCursors[nextCursor] = true
		nextID = out.NextRecordIdentifier
		nextName = out.NextRecordName
		nextType = out.NextRecordType
	}
}

func Route53ListRecords(ctx context.Context, zoneID string) ([]r53types.ResourceRecordSet, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Route53ListRecords"}
		d.Start()
		defer d.End()
	}
	records, err := route53ListRecords(ctx, Route53Client(), zoneID)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return records, nil
}

func Route53ListZones(ctx context.Context) ([]r53types.HostedZone, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "Route53ListZones"}
		d.Start()
		defer d.End()
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
