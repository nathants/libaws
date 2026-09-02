package lib

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

const (
	lambdaAPIDomainAPIIDTagName       = "libaws.api-id"
	lambdaAPIDomainRoute53ZoneTagName = "libaws.route53-zone"
)

type lambdaAPIDomainReader interface {
	GetDomainName(context.Context, *apigatewayv2.GetDomainNameInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.GetDomainNameOutput, error)
}

type lambdaAPIDomainOwnershipClient interface {
	lambdaAPIDomainReader
	TagResource(context.Context, *apigatewayv2.TagResourceInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.TagResourceOutput, error)
	UntagResource(context.Context, *apigatewayv2.UntagResourceInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.UntagResourceOutput, error)
}

type lambdaAPIMappingReader interface {
	GetApiMappings(context.Context, *apigatewayv2.GetApiMappingsInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.GetApiMappingsOutput, error)
}

type lambdaAPIDomainCleanupClient interface {
	lambdaAPIMappingReader
	DeleteApiMapping(context.Context, *apigatewayv2.DeleteApiMappingInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.DeleteApiMappingOutput, error)
	DeleteDomainName(context.Context, *apigatewayv2.DeleteDomainNameInput, ...func(*apigatewayv2.Options)) (*apigatewayv2.DeleteDomainNameOutput, error)
}

type lambdaAPIDNSClient interface {
	route53RecordListClient
	ChangeResourceRecordSets(context.Context, *route53.ChangeResourceRecordSetsInput, ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error)
}

func lambdaAPIDomainTags(infraSetName, apiID, route53ZoneID string) map[string]string {
	tags := map[string]string{
		infraSetTagName:             infraSetName,
		lambdaAPIDomainAPIIDTagName: apiID,
	}
	if route53ZoneID != "" {
		tags[lambdaAPIDomainRoute53ZoneTagName] = route53ZoneID
	}
	return tags
}

func lambdaAPIDomainFromGet(out *apigatewayv2.GetDomainNameOutput) *apitypes.DomainName {
	if out == nil {
		return nil
	}
	return &apitypes.DomainName{
		ApiMappingSelectionExpression: out.ApiMappingSelectionExpression,
		DomainName:                    out.DomainName,
		DomainNameArn:                 out.DomainNameArn,
		DomainNameConfigurations:      out.DomainNameConfigurations,
		MutualTlsAuthentication:       out.MutualTlsAuthentication,
		RoutingMode:                   out.RoutingMode,
		Tags:                          out.Tags,
	}
}

func lambdaAPIDomainConfigurationError(domain *apitypes.DomainName) error {
	if domain == nil || len(domain.DomainNameConfigurations) != 1 {
		return errors.New("API domain must have exactly one configuration")
	}
	configuration := domain.DomainNameConfigurations[0]
	if configuration.EndpointType != apitypes.EndpointTypeRegional {
		return fmt.Errorf("API domain endpoint type is %q, want %q", configuration.EndpointType, apitypes.EndpointTypeRegional)
	}
	if configuration.SecurityPolicy != apitypes.SecurityPolicyTls12 {
		return fmt.Errorf("API domain security policy is %q, want %q", configuration.SecurityPolicy, apitypes.SecurityPolicyTls12)
	}
	if domain.RoutingMode != apitypes.RoutingModeApiMappingOnly {
		return fmt.Errorf("API domain routing mode is %q, want %q", domain.RoutingMode, apitypes.RoutingModeApiMappingOnly)
	}
	if domain.MutualTlsAuthentication != nil {
		return errors.New("API domain uses unsupported mutual TLS authentication")
	}
	return nil
}

func lambdaAPIRootMapping(mapping *apitypes.ApiMapping) bool {
	return mapping != nil && aws.ToString(mapping.ApiId) != "" &&
		aws.ToString(mapping.ApiMappingId) != "" && aws.ToString(mapping.Stage) == lambdaDollarDefault &&
		aws.ToString(mapping.ApiMappingKey) == ""
}

func lambdaAPIListMappings(
	ctx context.Context,
	client lambdaAPIMappingReader,
	domainName string,
) ([]apitypes.ApiMapping, error) {
	if domainName == "" {
		return nil, errors.New("cannot list API mappings without a domain name")
	}
	var mappings []apitypes.ApiMapping
	var token *string
	seenTokens := map[string]bool{}
	for {
		out, err := client.GetApiMappings(ctx, &apigatewayv2.GetApiMappingsInput{
			DomainName: aws.String(domainName),
			MaxResults: aws.String("500"),
			NextToken:  token,
		})
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, errors.New("API Gateway GetApiMappings returned nil output")
		}
		for _, mapping := range out.Items {
			if aws.ToString(mapping.ApiId) == "" || aws.ToString(mapping.ApiMappingId) == "" ||
				aws.ToString(mapping.Stage) == "" {
				return nil, fmt.Errorf("API Gateway returned a mapping without stable identity for domain %q", domainName)
			}
			mappings = append(mappings, mapping)
		}
		if out.NextToken == nil {
			return mappings, nil
		}
		nextToken := aws.ToString(out.NextToken)
		if nextToken == "" || seenTokens[nextToken] {
			return nil, errors.New("API Gateway returned an invalid or repeated mapping pagination cursor")
		}
		seenTokens[nextToken] = true
		token = out.NextToken
	}
}

func lambdaEnsureAPIDomainOwnership(
	ctx context.Context,
	client lambdaAPIDomainOwnershipClient,
	dnsClient lambdaAPIDNSClient,
	domainName string,
	infraSetName string,
	apiID string,
	route53ZoneID string,
	preview bool,
) error {
	if infraSetName == "" {
		return errors.New("cannot own an API domain without an infrastructure-set name")
	}
	if apiID == "" && !preview {
		return errors.New("cannot own an API domain without an API ID")
	}
	out, err := client.GetDomainName(ctx, &apigatewayv2.GetDomainNameInput{DomainName: aws.String(domainName)})
	if err != nil {
		var notFound *apitypes.NotFoundException
		if preview && errors.As(err, &notFound) {
			return nil
		}
		return err
	}
	domain := lambdaAPIDomainFromGet(out)
	if domain == nil || domain.DomainName == nil || domain.DomainNameArn == nil ||
		aws.ToString(domain.DomainName) != domainName {
		return fmt.Errorf("API domain %q has no stable identity", domainName)
	}
	oldZoneID := domain.Tags[lambdaAPIDomainRoute53ZoneTagName]
	if oldZoneID != "" && oldZoneID != route53ZoneID {
		if _, err := lambdaDeleteAPIDNSRecord(ctx, dnsClient, oldZoneID, domain, preview); err != nil {
			return err
		}
	}
	updates := map[string]string{}
	if domain.Tags[infraSetTagName] != infraSetName {
		updates[infraSetTagName] = infraSetName
	}
	if apiID != "" && domain.Tags[lambdaAPIDomainAPIIDTagName] != apiID {
		updates[lambdaAPIDomainAPIIDTagName] = apiID
	}
	if route53ZoneID != "" && oldZoneID != route53ZoneID {
		updates[lambdaAPIDomainRoute53ZoneTagName] = route53ZoneID
	}
	if len(updates) != 0 {
		if !preview {
			if _, err := client.TagResource(ctx, &apigatewayv2.TagResourceInput{
				ResourceArn: domain.DomainNameArn,
				Tags:        updates,
			}); err != nil {
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"updated API domain infrastructure tags:", domainName)
	}
	if route53ZoneID == "" && oldZoneID != "" {
		if !preview {
			if _, err := client.UntagResource(ctx, &apigatewayv2.UntagResourceInput{
				ResourceArn: domain.DomainNameArn,
				TagKeys:     []string{lambdaAPIDomainRoute53ZoneTagName},
			}); err != nil {
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"removed API domain Route53 ownership tag:", domainName)
	}
	return nil
}

func lambdaAPIDNSHostedZone(domainName string, zones []route53types.HostedZone) (*route53types.HostedZone, error) {
	candidates := []string{domainName}
	if _, parent, err := SplitOnce(domainName, "."); err == nil {
		candidates = append(candidates, parent)
	}
	for _, candidate := range candidates {
		var matches []int
		for index := range zones {
			if route53DNSNameEqual(aws.ToString(zones[index].Name), candidate) {
				matches = append(matches, index)
			}
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("multiple hosted zones match API domain %q", domainName)
		}
		if len(matches) == 1 {
			zone := &zones[matches[0]]
			if aws.ToString(zone.Id) == "" {
				return nil, fmt.Errorf("hosted zone for API domain %q has no ID", domainName)
			}
			return zone, nil
		}
	}
	return nil, fmt.Errorf("no hosted zone matches API domain or parent domain: %s", domainName)
}

func lambdaAPIDNSDesiredRecord(domain *apitypes.DomainName) (*route53types.ResourceRecordSet, error) {
	if domain == nil || aws.ToString(domain.DomainName) == "" || len(domain.DomainNameConfigurations) != 1 {
		return nil, errors.New("API domain has no stable DNS configuration")
	}
	config := domain.DomainNameConfigurations[0]
	if aws.ToString(config.ApiGatewayDomainName) == "" || aws.ToString(config.HostedZoneId) == "" {
		return nil, fmt.Errorf("API domain %q has no stable DNS target", aws.ToString(domain.DomainName))
	}
	return &route53types.ResourceRecordSet{
		Name: domain.DomainName,
		Type: route53types.RRTypeA,
		AliasTarget: &route53types.AliasTarget{
			DNSName:              config.ApiGatewayDomainName,
			HostedZoneId:         config.HostedZoneId,
			EvaluateTargetHealth: false,
		},
	}, nil
}

func lambdaAPIDNSRecordMatches(
	domain *apitypes.DomainName,
	record *route53types.ResourceRecordSet,
) bool {
	desired, err := lambdaAPIDNSDesiredRecord(domain)
	if err != nil || record == nil || record.Type != route53types.RRTypeA ||
		!route53RecordIsSimple(record) || record.AliasTarget == nil {
		return false
	}
	alias := record.AliasTarget
	return route53DNSNameEqual(aws.ToString(record.Name), aws.ToString(desired.Name)) &&
		route53DNSNameEqual(aws.ToString(alias.DNSName), aws.ToString(desired.AliasTarget.DNSName)) &&
		aws.ToString(alias.HostedZoneId) == aws.ToString(desired.AliasTarget.HostedZoneId) &&
		!alias.EvaluateTargetHealth
}

func lambdaAPIDomainForDNSRecord(
	domains []apitypes.DomainName,
	record *route53types.ResourceRecordSet,
) *apitypes.DomainName {
	for index := range domains {
		if lambdaAPIDNSRecordMatches(&domains[index], record) {
			return &domains[index]
		}
	}
	return nil
}

func lambdaDeleteAPIDNSRecord(
	ctx context.Context,
	client lambdaAPIDNSClient,
	zoneID string,
	domain *apitypes.DomainName,
	preview bool,
) (bool, error) {
	if zoneID == "" || domain == nil || domain.DomainName == nil {
		return false, nil
	}
	records, err := route53ListRecords(ctx, client, zoneID)
	if err != nil {
		var notFound *route53types.NoSuchHostedZone
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, err
	}
	var matches []route53types.ResourceRecordSet
	for index := range records {
		if lambdaAPIDNSRecordMatches(domain, &records[index]) {
			matches = append(matches, records[index])
		}
	}
	if len(matches) > 1 {
		return false, fmt.Errorf("multiple libaws-shaped Route53 aliases match API domain %q", aws.ToString(domain.DomainName))
	}
	if len(matches) == 0 {
		return false, nil
	}
	if !preview {
		if _, err := client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: aws.String(zoneID),
			ChangeBatch: &route53types.ChangeBatch{Changes: []route53types.Change{{
				Action:            route53types.ChangeActionDelete,
				ResourceRecordSet: &matches[0],
			}}},
		}); err != nil {
			return false, err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted API DNS record:", aws.ToString(domain.DomainName))
	return true, nil
}

var _ lambdaAPIDomainOwnershipClient = (*apigatewayv2.Client)(nil)
var _ lambdaAPIDomainCleanupClient = (*apigatewayv2.Client)(nil)
var _ lambdaAPIDNSClient = (*route53.Client)(nil)
