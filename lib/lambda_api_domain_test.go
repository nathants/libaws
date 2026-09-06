package lib

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

type fakeLambdaAPIDomainOwnershipClient struct {
	domain   *apigatewayv2.GetDomainNameOutput
	tagged   []*apigatewayv2.TagResourceInput
	untagged []*apigatewayv2.UntagResourceInput
}

func (client *fakeLambdaAPIDomainOwnershipClient) GetDomainName(
	context.Context,
	*apigatewayv2.GetDomainNameInput,
	...func(*apigatewayv2.Options),
) (*apigatewayv2.GetDomainNameOutput, error) {
	return client.domain, nil
}

func (client *fakeLambdaAPIDomainOwnershipClient) TagResource(
	_ context.Context,
	input *apigatewayv2.TagResourceInput,
	_ ...func(*apigatewayv2.Options),
) (*apigatewayv2.TagResourceOutput, error) {
	client.tagged = append(client.tagged, input)
	return &apigatewayv2.TagResourceOutput{}, nil
}

func (client *fakeLambdaAPIDomainOwnershipClient) UntagResource(
	_ context.Context,
	input *apigatewayv2.UntagResourceInput,
	_ ...func(*apigatewayv2.Options),
) (*apigatewayv2.UntagResourceOutput, error) {
	client.untagged = append(client.untagged, input)
	return &apigatewayv2.UntagResourceOutput{}, nil
}

type fakeLambdaAPIDNSClient struct {
	pages   map[string]*route53.ListResourceRecordSetsOutput
	changes []*route53.ChangeResourceRecordSetsInput
}

func (client *fakeLambdaAPIDNSClient) ListResourceRecordSets(
	_ context.Context,
	input *route53.ListResourceRecordSetsInput,
	_ ...func(*route53.Options),
) (*route53.ListResourceRecordSetsOutput, error) {
	return client.pages[aws.ToString(input.StartRecordName)], nil
}

func (client *fakeLambdaAPIDNSClient) ChangeResourceRecordSets(
	_ context.Context,
	input *route53.ChangeResourceRecordSetsInput,
	_ ...func(*route53.Options),
) (*route53.ChangeResourceRecordSetsOutput, error) {
	client.changes = append(client.changes, input)
	return &route53.ChangeResourceRecordSetsOutput{}, nil
}

func lambdaAPIDomainTestState(tags map[string]string) *apigatewayv2.GetDomainNameOutput {
	return &apigatewayv2.GetDomainNameOutput{
		DomainName:    aws.String("api.example.com"),
		DomainNameArn: aws.String("arn:aws:apigateway:us-west-2::/domainnames/api.example.com"),
		RoutingMode:   apitypes.RoutingModeApiMappingOnly,
		DomainNameConfigurations: []apitypes.DomainNameConfiguration{{
			ApiGatewayDomainName: aws.String("d-abc.execute-api.us-west-2.amazonaws.com"),
			HostedZoneId:         aws.String("Z2OJLYMUO9EFXC"),
			EndpointType:         apitypes.EndpointTypeRegional,
			SecurityPolicy:       apitypes.SecurityPolicyTls12,
		}},
		Tags: tags,
	}
}

func lambdaAPIDomainTestRecord() route53types.ResourceRecordSet {
	return route53types.ResourceRecordSet{
		Name: aws.String("api.example.com."),
		Type: route53types.RRTypeA,
		AliasTarget: &route53types.AliasTarget{
			DNSName:              aws.String("d-abc.execute-api.us-west-2.amazonaws.com."),
			HostedZoneId:         aws.String("Z2OJLYMUO9EFXC"),
			EvaluateTargetHealth: false,
		},
	}
}

func TestLambdaAPIDomainConfigurationRejectsRoutingRulesAndMutualTLS(t *testing.T) {
	domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(nil))
	if err := lambdaAPIDomainConfigurationError(domain); err != nil {
		t.Fatalf("simple API domain configuration: %v", err)
	}
	domain.RoutingMode = apitypes.RoutingModeRoutingRuleOnly
	if err := lambdaAPIDomainConfigurationError(domain); err == nil || !strings.Contains(err.Error(), "routing mode") {
		t.Fatalf("routing-mode error = %v", err)
	}
	domain.RoutingMode = apitypes.RoutingModeApiMappingOnly
	domain.MutualTlsAuthentication = &apitypes.MutualTlsAuthentication{}
	if err := lambdaAPIDomainConfigurationError(domain); err == nil || !strings.Contains(err.Error(), "mutual TLS") {
		t.Fatalf("mutual-TLS error = %v", err)
	}
}

func TestLambdaAPIDNSHostedZoneSelectsUniqueExactOrParentZone(t *testing.T) {
	zones := []route53types.HostedZone{
		{Name: aws.String("example.com."), Id: aws.String("/hostedzone/PARENT")},
		{Name: aws.String("api.example.com."), Id: aws.String("/hostedzone/EXACT")},
	}
	zone, err := lambdaAPIDNSHostedZone("api.example.com", zones)
	if err != nil || aws.ToString(zone.Id) != "/hostedzone/EXACT" {
		t.Fatalf("exact zone = %#v, error = %v", zone, err)
	}
	zone, err = lambdaAPIDNSHostedZone("other.example.com", zones)
	if err != nil || aws.ToString(zone.Id) != "/hostedzone/PARENT" {
		t.Fatalf("parent zone = %#v, error = %v", zone, err)
	}
	zones = append(zones, route53types.HostedZone{
		Name: aws.String("api.example.com."), Id: aws.String("/hostedzone/DUPLICATE"),
	})
	if _, err := lambdaAPIDNSHostedZone("api.example.com", zones); err == nil ||
		!strings.Contains(err.Error(), "multiple hosted zones") {
		t.Fatalf("duplicate-zone error = %v", err)
	}
}

func TestLambdaEnsureAPIDomainOwnershipTagsAdoptedDomainAndDNSZone(t *testing.T) {
	apiClient := &fakeLambdaAPIDomainOwnershipClient{domain: lambdaAPIDomainTestState(nil)}
	dnsClient := &fakeLambdaAPIDNSClient{}
	if err := lambdaEnsureAPIDomainOwnership(
		context.Background(), apiClient, dnsClient,
		"api.example.com", "set", "api-id", "/hostedzone/Z123", false,
	); err != nil {
		t.Fatal(err)
	}
	if len(apiClient.tagged) != 1 || len(apiClient.untagged) != 0 {
		t.Fatalf("tag calls = %#v, untag calls = %#v", apiClient.tagged, apiClient.untagged)
	}
	input := apiClient.tagged[0]
	if aws.ToString(input.ResourceArn) != "arn:aws:apigateway:us-west-2::/domainnames/api.example.com" ||
		!reflect.DeepEqual(input.Tags, map[string]string{
			infraSetTagName:                   "set",
			lambdaAPIDomainAPIIDTagName:       "api-id",
			lambdaAPIDomainRoute53ZoneTagName: "/hostedzone/Z123",
		}) {
		t.Fatalf("domain tags = %#v", input)
	}
}

func TestLambdaEnsureAPIDomainOwnershipRemovesManagedDNSWhenChangingToDomainOnly(t *testing.T) {
	apiClient := &fakeLambdaAPIDomainOwnershipClient{domain: lambdaAPIDomainTestState(map[string]string{
		infraSetTagName:                   "set",
		lambdaAPIDomainAPIIDTagName:       "api-id",
		lambdaAPIDomainRoute53ZoneTagName: "/hostedzone/Z123",
	})}
	record := lambdaAPIDomainTestRecord()
	unowned := record
	unowned.SetIdentifier = aws.String("weighted")
	unowned.Weight = aws.Int64(1)
	dnsClient := &fakeLambdaAPIDNSClient{pages: map[string]*route53.ListResourceRecordSetsOutput{
		"": {ResourceRecordSets: []route53types.ResourceRecordSet{unowned, record}},
	}}
	if err := lambdaEnsureAPIDomainOwnership(
		context.Background(), apiClient, dnsClient,
		"api.example.com", "set", "api-id", "", false,
	); err != nil {
		t.Fatal(err)
	}
	if len(dnsClient.changes) != 1 || len(dnsClient.changes[0].ChangeBatch.Changes) != 1 ||
		!reflect.DeepEqual(dnsClient.changes[0].ChangeBatch.Changes[0].ResourceRecordSet, &record) {
		t.Fatalf("Route53 deletes = %#v", dnsClient.changes)
	}
	if len(apiClient.tagged) != 0 || len(apiClient.untagged) != 1 ||
		!reflect.DeepEqual(apiClient.untagged[0].TagKeys, []string{lambdaAPIDomainRoute53ZoneTagName}) {
		t.Fatalf("tag calls = %#v, untag calls = %#v", apiClient.tagged, apiClient.untagged)
	}
}

func TestLambdaAPIDNSRecordMatchesEscapedWildcardName(t *testing.T) {
	domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(nil))
	domain.DomainName = aws.String("*.example.com")
	record := lambdaAPIDomainTestRecord()
	record.Name = aws.String("\\052.example.com.")
	if !lambdaAPIDNSRecordMatches(domain, &record) {
		t.Fatalf("escaped wildcard record did not match API domain: %#v", record)
	}
}

func TestLambdaAPIDomainForDNSRecordRequiresExactManagedAlias(t *testing.T) {
	domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(nil))
	record := lambdaAPIDomainTestRecord()
	if got := lambdaAPIDomainForDNSRecord([]apitypes.DomainName{*domain}, &record); got == nil ||
		aws.ToString(got.DomainName) != "api.example.com" {
		t.Fatalf("managed alias domain = %#v", got)
	}
	record.AliasTarget = nil
	record.TTL = aws.Int64(60)
	record.ResourceRecords = []route53types.ResourceRecord{{Value: aws.String("192.0.2.1")}}
	if got := lambdaAPIDomainForDNSRecord([]apitypes.DomainName{*domain}, &record); got != nil {
		t.Fatalf("ordinary A record was represented as managed API DNS: %#v", got)
	}
}

func TestLambdaDeleteAPIDNSRecordPaginatesAndDeletesOnlyExactManagedShape(t *testing.T) {
	domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(nil))
	record := lambdaAPIDomainTestRecord()
	wrongTarget := record
	wrongTarget.AliasTarget = &route53types.AliasTarget{
		DNSName:      aws.String("other.example.com."),
		HostedZoneId: record.AliasTarget.HostedZoneId,
	}
	client := &fakeLambdaAPIDNSClient{pages: map[string]*route53.ListResourceRecordSetsOutput{
		"": {
			IsTruncated:        true,
			ResourceRecordSets: []route53types.ResourceRecordSet{wrongTarget},
			NextRecordName:     aws.String("next.example.com."),
			NextRecordType:     route53types.RRTypeA,
		},
		"next.example.com.": {ResourceRecordSets: []route53types.ResourceRecordSet{record}},
	}}
	deleted, err := lambdaDeleteAPIDNSRecord(context.Background(), client, "/hostedzone/Z123", domain, false)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted || len(client.changes) != 1 ||
		!reflect.DeepEqual(client.changes[0].ChangeBatch.Changes[0].ResourceRecordSet, &record) {
		t.Fatalf("deleted = %v, changes = %#v", deleted, client.changes)
	}
}

func TestLambdaEnsureTriggerApiDNSRecordsConvergesSimpleARecord(t *testing.T) {
	apiClient := &fakeLambdaAPIDomainOwnershipClient{domain: lambdaAPIDomainTestState(nil)}
	dnsClient := &fakeLambdaAPIDNSClient{pages: map[string]*route53.ListResourceRecordSetsOutput{
		"": {ResourceRecordSets: []route53types.ResourceRecordSet{{
			Name:            aws.String("api.example.com."),
			Type:            route53types.RRTypeA,
			TTL:             aws.Int64(300),
			ResourceRecords: []route53types.ResourceRecord{{Value: aws.String("192.0.2.1")}},
		}}},
	}}
	zone := route53types.HostedZone{Id: aws.String("/hostedzone/Z123")}
	if err := lambdaEnsureTriggerApiDnsRecordsWith(
		context.Background(), apiClient, dnsClient, "function", "api.example.com", zone, false,
	); err != nil {
		t.Fatal(err)
	}
	want := lambdaAPIDomainTestRecord()
	want.Name = aws.String("api.example.com")
	want.AliasTarget.DNSName = aws.String("d-abc.execute-api.us-west-2.amazonaws.com")
	if len(dnsClient.changes) != 1 || len(dnsClient.changes[0].ChangeBatch.Changes) != 1 ||
		dnsClient.changes[0].ChangeBatch.Changes[0].Action != route53types.ChangeActionUpsert ||
		!reflect.DeepEqual(dnsClient.changes[0].ChangeBatch.Changes[0].ResourceRecordSet, &want) {
		t.Fatalf("Route53 changes = %#v, want UPSERT %#v", dnsClient.changes, want)
	}
}

func TestLambdaEnsureTriggerApiDNSRecordsPreservesUnsupportedRecords(t *testing.T) {
	exact := lambdaAPIDomainTestRecord()
	weighted := exact
	weighted.SetIdentifier = aws.String("weighted")
	weighted.Weight = aws.Int64(1)
	secondWeighted := weighted
	secondWeighted.SetIdentifier = aws.String("weighted-2")
	cname := route53types.ResourceRecordSet{
		Name:            aws.String("api.example.com."),
		Type:            route53types.RRTypeCname,
		TTL:             aws.Int64(300),
		ResourceRecords: []route53types.ResourceRecord{{Value: aws.String("other.example.com.")}},
	}
	tests := []struct {
		name    string
		records []route53types.ResourceRecordSet
		wantErr string
	}{
		{name: "exact managed alias", records: []route53types.ResourceRecordSet{exact}},
		{name: "weighted alias", records: []route53types.ResourceRecordSet{weighted}, wantErr: "unsupported routing"},
		{name: "multiple weighted aliases", records: []route53types.ResourceRecordSet{weighted, secondWeighted}, wantErr: "multiple Route53 A records"},
		{name: "CNAME", records: []route53types.ResourceRecordSet{cname}, wantErr: "CNAME conflicts"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiClient := &fakeLambdaAPIDomainOwnershipClient{domain: lambdaAPIDomainTestState(nil)}
			dnsClient := &fakeLambdaAPIDNSClient{pages: map[string]*route53.ListResourceRecordSetsOutput{
				"": {ResourceRecordSets: test.records},
			}}
			err := lambdaEnsureTriggerApiDnsRecordsWith(
				context.Background(), apiClient, dnsClient, "function", "api.example.com",
				route53types.HostedZone{Id: aws.String("/hostedzone/Z123")}, false,
			)
			if test.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
			if len(dnsClient.changes) != 0 {
				t.Fatalf("unexpected Route53 changes: %#v", dnsClient.changes)
			}
		})
	}
}

type fakeLambdaAPIDomainCleanupClient struct {
	mappings       *apigatewayv2.GetApiMappingsOutput
	mappingPages   map[string]*apigatewayv2.GetApiMappingsOutput
	mappingInputs  []*apigatewayv2.GetApiMappingsInput
	deletedMapping []*apigatewayv2.DeleteApiMappingInput
	deletedDomain  []*apigatewayv2.DeleteDomainNameInput
}

func (client *fakeLambdaAPIDomainCleanupClient) GetApiMappings(
	_ context.Context,
	input *apigatewayv2.GetApiMappingsInput,
	_ ...func(*apigatewayv2.Options),
) (*apigatewayv2.GetApiMappingsOutput, error) {
	client.mappingInputs = append(client.mappingInputs, input)
	if client.mappingPages != nil {
		return client.mappingPages[aws.ToString(input.NextToken)], nil
	}
	return client.mappings, nil
}

func (client *fakeLambdaAPIDomainCleanupClient) DeleteApiMapping(
	_ context.Context,
	input *apigatewayv2.DeleteApiMappingInput,
	_ ...func(*apigatewayv2.Options),
) (*apigatewayv2.DeleteApiMappingOutput, error) {
	client.deletedMapping = append(client.deletedMapping, input)
	return &apigatewayv2.DeleteApiMappingOutput{}, nil
}

func (client *fakeLambdaAPIDomainCleanupClient) DeleteDomainName(
	_ context.Context,
	input *apigatewayv2.DeleteDomainNameInput,
	_ ...func(*apigatewayv2.Options),
) (*apigatewayv2.DeleteDomainNameOutput, error) {
	client.deletedDomain = append(client.deletedDomain, input)
	return &apigatewayv2.DeleteDomainNameOutput{}, nil
}

func lambdaAPIDomainCleanupTestMapping() apitypes.ApiMapping {
	return apitypes.ApiMapping{
		ApiId:        aws.String("api-id"),
		ApiMappingId: aws.String("mapping-id"),
		Stage:        aws.String(lambdaDollarDefault),
	}
}

func TestLambdaAPIRootMappingRejectsNonemptyMappingKey(t *testing.T) {
	mapping := lambdaAPIDomainCleanupTestMapping()
	if !lambdaAPIRootMapping(&mapping) {
		t.Fatal("root API mapping did not match")
	}
	mapping.ApiMappingKey = aws.String("nested")
	if lambdaAPIRootMapping(&mapping) {
		t.Fatal("non-root API mapping matched the libaws root mapping shape")
	}
}

func TestLambdaAPIListMappingsPaginatesWithStableCursor(t *testing.T) {
	first := lambdaAPIDomainCleanupTestMapping()
	second := lambdaAPIDomainCleanupTestMapping()
	second.ApiId = aws.String("other-api")
	second.ApiMappingId = aws.String("other-mapping")
	client := &fakeLambdaAPIDomainCleanupClient{mappingPages: map[string]*apigatewayv2.GetApiMappingsOutput{
		"":     {Items: []apitypes.ApiMapping{first}, NextToken: aws.String("next")},
		"next": {Items: []apitypes.ApiMapping{second}},
	}}
	mappings, err := lambdaAPIListMappings(context.Background(), client, "api.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mappings, []apitypes.ApiMapping{first, second}) {
		t.Fatalf("mappings = %#v", mappings)
	}
	if len(client.mappingInputs) != 2 ||
		aws.ToString(client.mappingInputs[0].DomainName) != "api.example.com" ||
		aws.ToString(client.mappingInputs[0].MaxResults) != "500" ||
		client.mappingInputs[0].NextToken != nil ||
		aws.ToString(client.mappingInputs[1].NextToken) != "next" {
		t.Fatalf("mapping inputs = %#v", client.mappingInputs)
	}
}

func TestLambdaAPIListMappingsRejectsInvalidPagination(t *testing.T) {
	tests := []struct {
		name  string
		pages map[string]*apigatewayv2.GetApiMappingsOutput
	}{
		{name: "nil output", pages: map[string]*apigatewayv2.GetApiMappingsOutput{"": nil}},
		{name: "empty cursor", pages: map[string]*apigatewayv2.GetApiMappingsOutput{
			"": {NextToken: aws.String("")},
		}},
		{name: "repeated cursor", pages: map[string]*apigatewayv2.GetApiMappingsOutput{
			"":       {NextToken: aws.String("repeat")},
			"repeat": {NextToken: aws.String("repeat")},
		}},
		{name: "cyclic cursor", pages: map[string]*apigatewayv2.GetApiMappingsOutput{
			"":       {NextToken: aws.String("first")},
			"first":  {NextToken: aws.String("second")},
			"second": {NextToken: aws.String("first")},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeLambdaAPIDomainCleanupClient{mappingPages: test.pages}
			if _, err := lambdaAPIListMappings(context.Background(), client, "api.example.com"); err == nil {
				t.Fatal("expected pagination error")
			}
		})
	}
}

func TestLambdaAPIListMappingsRejectsMappingWithoutStableIdentity(t *testing.T) {
	mapping := lambdaAPIDomainCleanupTestMapping()
	mapping.ApiMappingId = nil
	client := &fakeLambdaAPIDomainCleanupClient{mappings: &apigatewayv2.GetApiMappingsOutput{
		Items: []apitypes.ApiMapping{mapping},
	}}
	if _, err := lambdaAPIListMappings(context.Background(), client, "api.example.com"); err == nil ||
		!strings.Contains(err.Error(), "without stable identity") {
		t.Fatalf("error = %v", err)
	}
}

func TestLambdaAPIDomainCleanupAfterManualLambdaRemovalDeletesOwnedDomainAndDNS(t *testing.T) {
	apiClient := &fakeLambdaAPIDomainCleanupClient{mappings: &apigatewayv2.GetApiMappingsOutput{
		Items: []apitypes.ApiMapping{lambdaAPIDomainCleanupTestMapping()},
	}}
	record := lambdaAPIDomainTestRecord()
	dnsClient := &fakeLambdaAPIDNSClient{pages: map[string]*route53.ListResourceRecordSetsOutput{
		"": {ResourceRecordSets: []route53types.ResourceRecordSet{record}},
	}}
	domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(map[string]string{
		infraSetTagName:                   "set",
		lambdaAPIDomainAPIIDTagName:       "api-id",
		lambdaAPIDomainRoute53ZoneTagName: "/hostedzone/Z123",
	}))
	if err := lambdaTriggerApiDeleteDnsWith(
		context.Background(), apiClient, dnsClient, "function",
		&apitypes.Api{ApiId: aws.String("api-id")}, *domain, "set", false,
	); err != nil {
		t.Fatal(err)
	}
	if len(apiClient.deletedDomain) != 1 || len(apiClient.deletedMapping) != 0 || len(dnsClient.changes) != 1 {
		t.Fatalf(
			"domain deletes = %#v, mapping deletes = %#v, DNS deletes = %#v",
			apiClient.deletedDomain, apiClient.deletedMapping, dnsClient.changes,
		)
	}
}

func TestLambdaAPIDomainCleanupAfterManualLambdaRemovalDeletesOwnedDomainWithoutDNS(t *testing.T) {
	apiClient := &fakeLambdaAPIDomainCleanupClient{mappings: &apigatewayv2.GetApiMappingsOutput{
		Items: []apitypes.ApiMapping{lambdaAPIDomainCleanupTestMapping()},
	}}
	domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(map[string]string{
		infraSetTagName:             "set",
		lambdaAPIDomainAPIIDTagName: "api-id",
	}))
	if err := lambdaTriggerApiDeleteDnsWith(
		context.Background(), apiClient, &fakeLambdaAPIDNSClient{}, "function",
		&apitypes.Api{ApiId: aws.String("api-id")}, *domain, "set", false,
	); err != nil {
		t.Fatal(err)
	}
	if len(apiClient.deletedDomain) != 1 || len(apiClient.deletedMapping) != 0 {
		t.Fatalf("domain deletes = %#v, mapping deletes = %#v", apiClient.deletedDomain, apiClient.deletedMapping)
	}
}

func TestLambdaAPIDomainCleanupDeletesOwnedUnmappedDomainAndDNS(t *testing.T) {
	apiClient := &fakeLambdaAPIDomainCleanupClient{mappings: &apigatewayv2.GetApiMappingsOutput{}}
	record := lambdaAPIDomainTestRecord()
	dnsClient := &fakeLambdaAPIDNSClient{pages: map[string]*route53.ListResourceRecordSetsOutput{
		"": {ResourceRecordSets: []route53types.ResourceRecordSet{record}},
	}}
	domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(map[string]string{
		infraSetTagName:                   "set",
		lambdaAPIDomainAPIIDTagName:       "api-id",
		lambdaAPIDomainRoute53ZoneTagName: "/hostedzone/Z123",
	}))
	if err := lambdaTriggerApiDeleteDnsWith(
		context.Background(), apiClient, dnsClient, "function",
		&apitypes.Api{ApiId: aws.String("api-id")}, *domain, "set", false,
	); err != nil {
		t.Fatal(err)
	}
	if len(apiClient.deletedDomain) != 1 || len(apiClient.deletedMapping) != 0 || len(dnsClient.changes) != 1 {
		t.Fatalf(
			"domain deletes = %#v, mapping deletes = %#v, DNS deletes = %#v",
			apiClient.deletedDomain, apiClient.deletedMapping, dnsClient.changes,
		)
	}
}

func TestLambdaAPIDomainCleanupPreservesUnsupportedRoutingModes(t *testing.T) {
	for _, mode := range []apitypes.RoutingMode{"", "UNKNOWN", apitypes.RoutingModeRoutingRuleOnly, apitypes.RoutingModeRoutingRuleThenApiMapping} {
		for _, mapped := range []bool{false, true} {
			for _, preview := range []bool{false, true} {
				t.Run(string(mode)+"/mapped="+Json(mapped)+"/preview="+Json(preview), func(t *testing.T) {
					apiClient := &fakeLambdaAPIDomainCleanupClient{mappings: &apigatewayv2.GetApiMappingsOutput{}}
					if mapped {
						apiClient.mappings.Items = []apitypes.ApiMapping{lambdaAPIDomainCleanupTestMapping()}
					}
					dnsClient := &fakeLambdaAPIDNSClient{pages: map[string]*route53.ListResourceRecordSetsOutput{
						"": {ResourceRecordSets: []route53types.ResourceRecordSet{lambdaAPIDomainTestRecord()}},
					}}
					domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(map[string]string{
						infraSetTagName: "set", lambdaAPIDomainAPIIDTagName: "api-id", lambdaAPIDomainRoute53ZoneTagName: "/hostedzone/Z123",
					}))
					domain.RoutingMode = mode
					if err := lambdaTriggerApiDeleteDnsWith(context.Background(), apiClient, dnsClient, "function", &apitypes.Api{ApiId: aws.String("api-id")}, *domain, "set", preview); err != nil {
						t.Fatal(err)
					}
					wantMappings := 0
					if mapped && !preview {
						wantMappings = 1
					}
					if len(apiClient.deletedDomain) != 0 || len(dnsClient.changes) != 0 || len(apiClient.deletedMapping) != wantMappings {
						t.Fatalf("unsupported routing mode cleanup: domains=%v DNS=%v mappings=%v", apiClient.deletedDomain, dnsClient.changes, apiClient.deletedMapping)
					}
				})
			}
		}
	}
}

func TestLambdaAPIDomainCleanupPreservesSameSetUnmappedDomainOwnedByAnotherAPI(t *testing.T) {
	apiClient := &fakeLambdaAPIDomainCleanupClient{mappings: &apigatewayv2.GetApiMappingsOutput{}}
	dnsClient := &fakeLambdaAPIDNSClient{}
	domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(map[string]string{
		infraSetTagName:                   "set",
		lambdaAPIDomainAPIIDTagName:       "other-api",
		lambdaAPIDomainRoute53ZoneTagName: "/hostedzone/Z123",
	}))
	if err := lambdaTriggerApiDeleteDnsWith(
		context.Background(), apiClient, dnsClient, "function",
		&apitypes.Api{ApiId: aws.String("api-id")}, *domain, "set", false,
	); err != nil {
		t.Fatal(err)
	}
	if len(apiClient.deletedDomain) != 0 || len(apiClient.deletedMapping) != 0 || len(dnsClient.changes) != 0 {
		t.Fatalf(
			"domain deletes = %#v, mapping deletes = %#v, DNS deletes = %#v",
			apiClient.deletedDomain, apiClient.deletedMapping, dnsClient.changes,
		)
	}
}

func TestLambdaAPIDomainCleanupPreservesUnownedUnmappedDomainAndDNS(t *testing.T) {
	apiClient := &fakeLambdaAPIDomainCleanupClient{mappings: &apigatewayv2.GetApiMappingsOutput{}}
	dnsClient := &fakeLambdaAPIDNSClient{}
	domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(map[string]string{
		infraSetTagName:                   "other-set",
		lambdaAPIDomainAPIIDTagName:       "api-id",
		lambdaAPIDomainRoute53ZoneTagName: "/hostedzone/Z123",
	}))
	if err := lambdaTriggerApiDeleteDnsWith(
		context.Background(), apiClient, dnsClient, "function",
		&apitypes.Api{ApiId: aws.String("api-id")}, *domain, "set", false,
	); err != nil {
		t.Fatal(err)
	}
	if len(apiClient.deletedDomain) != 0 || len(apiClient.deletedMapping) != 0 || len(dnsClient.changes) != 0 {
		t.Fatalf(
			"domain deletes = %#v, mapping deletes = %#v, DNS deletes = %#v",
			apiClient.deletedDomain, apiClient.deletedMapping, dnsClient.changes,
		)
	}
}

func TestLambdaAPIDomainCleanupPreservesOwnedDomainWithAdditionalMapping(t *testing.T) {
	managed := lambdaAPIDomainCleanupTestMapping()
	apiClient := &fakeLambdaAPIDomainCleanupClient{mappings: &apigatewayv2.GetApiMappingsOutput{
		Items: []apitypes.ApiMapping{
			managed,
			{ApiId: aws.String("other-api"), ApiMappingId: aws.String("other-mapping"), Stage: aws.String(lambdaDollarDefault)},
		},
	}}
	domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(map[string]string{
		infraSetTagName:                   "set",
		lambdaAPIDomainAPIIDTagName:       "api-id",
		lambdaAPIDomainRoute53ZoneTagName: "/hostedzone/Z123",
	}))
	if err := lambdaTriggerApiDeleteDnsWith(
		context.Background(), apiClient, &fakeLambdaAPIDNSClient{}, "function",
		&apitypes.Api{ApiId: aws.String("api-id")}, *domain, "set", false,
	); err != nil {
		t.Fatal(err)
	}
	if len(apiClient.deletedDomain) != 0 || len(apiClient.deletedMapping) != 1 ||
		aws.ToString(apiClient.deletedMapping[0].ApiMappingId) != "mapping-id" {
		t.Fatalf("domain deletes = %#v, mapping deletes = %#v", apiClient.deletedDomain, apiClient.deletedMapping)
	}
}

func TestLambdaAPIDomainCleanupAfterManualLambdaRemovalPreservesUnownedDomainAndDNS(t *testing.T) {
	apiClient := &fakeLambdaAPIDomainCleanupClient{mappings: &apigatewayv2.GetApiMappingsOutput{
		Items: []apitypes.ApiMapping{lambdaAPIDomainCleanupTestMapping()},
	}}
	dnsClient := &fakeLambdaAPIDNSClient{}
	domain := lambdaAPIDomainFromGet(lambdaAPIDomainTestState(map[string]string{
		infraSetTagName:                   "other-set",
		lambdaAPIDomainAPIIDTagName:       "api-id",
		lambdaAPIDomainRoute53ZoneTagName: "/hostedzone/Z123",
	}))
	if err := lambdaTriggerApiDeleteDnsWith(
		context.Background(), apiClient, dnsClient, "function",
		&apitypes.Api{ApiId: aws.String("api-id")}, *domain, "set", false,
	); err != nil {
		t.Fatal(err)
	}
	if len(apiClient.deletedDomain) != 0 || len(apiClient.deletedMapping) != 1 || len(dnsClient.changes) != 0 {
		t.Fatalf(
			"domain deletes = %#v, mapping deletes = %#v, DNS deletes = %#v",
			apiClient.deletedDomain, apiClient.deletedMapping, dnsClient.changes,
		)
	}
}
