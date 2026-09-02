package lib

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

type lambdaAPIDomainIntegrationNames struct {
	infraSet          string
	httpFunction      string
	websocketFunction string
	httpAPI           string
	websocketAPI      string
	httpDomain        string
	websocketDomain   string
	fixtureDomain     string
	zone              string
}

func lambdaAPIDomainIntegrationIdentity(t *testing.T) lambdaAPIDomainIntegrationNames {
	t.Helper()
	uid := os.Getenv("LIBAWS_API_DOMAIN_TEST_UID")
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(uid) {
		t.Fatalf("LIBAWS_API_DOMAIN_TEST_UID must be 12 lowercase hexadecimal characters, got %q", uid)
	}
	zone := os.Getenv("LIBAWS_TEST_DOMAIN")
	if len(zone) > 230 || !regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`).MatchString(zone) {
		t.Fatalf("LIBAWS_TEST_DOMAIN must name a lowercase delegated test domain, got %q", zone)
	}
	httpFunction := "test-lambda-api-domain-http-" + uid
	websocketFunction := "test-lambda-api-domain-websocket-" + uid
	return lambdaAPIDomainIntegrationNames{
		infraSet:          "test-api-domain-set-" + uid,
		httpFunction:      httpFunction,
		websocketFunction: websocketFunction,
		httpAPI:           httpFunction,
		websocketAPI:      websocketFunction + LambdaWebsocketSuffix,
		httpDomain:        "api-" + uid + "." + zone,
		websocketDomain:   "ws-" + uid + "." + zone,
		fixtureDomain:     "acm-fixture." + zone,
		zone:              zone,
	}
}

func lambdaAPIDomainIntegrationAccount(t *testing.T, ctx context.Context) {
	t.Helper()
	expected := os.Getenv("LIBAWS_TEST_ACCOUNT")
	if expected == "" {
		t.Fatal("LIBAWS_TEST_ACCOUNT must identify the authorized scratch account")
	}
	actual, err := StsAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("refusing API domain integration operation in account %q; expected %q", actual, expected)
	}
}

func lambdaAPIDomainIntegrationWait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func lambdaAPIDomainIntegrationZone(ctx context.Context, name string) (*route53types.HostedZone, error) {
	zones, err := Route53ListZones(ctx)
	if err != nil {
		return nil, err
	}
	var matches []route53types.HostedZone
	for _, zone := range zones {
		if route53DNSNameEqual(aws.ToString(zone.Name), name) {
			matches = append(matches, zone)
		}
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple hosted zones named %q", name)
	}
	if len(matches) == 0 {
		return nil, nil
	}
	return &matches[0], nil
}

func lambdaAPIDomainIntegrationRequireZone(t *testing.T, ctx context.Context, name string) route53types.HostedZone {
	t.Helper()
	zone, err := lambdaAPIDomainIntegrationZone(ctx, name)
	if err != nil || zone == nil || aws.ToString(zone.Id) == "" {
		t.Fatalf("LIBAWS_TEST_DOMAIN hosted zone %q is unavailable: %#v, %v", name, zone, err)
	}
	return *zone
}

func lambdaAPIDomainIntegrationDescribeCertificate(
	ctx context.Context,
	certificateARN string,
) (*acmtypes.CertificateDetail, error) {
	if certificateARN == "" {
		return nil, errors.New("ACM certificate has no ARN")
	}
	out, err := AcmClient().DescribeCertificate(ctx, &acm.DescribeCertificateInput{
		CertificateArn: aws.String(certificateARN),
	})
	if err != nil {
		return nil, err
	}
	if out == nil || out.Certificate == nil || aws.ToString(out.Certificate.CertificateArn) != certificateARN {
		return nil, fmt.Errorf("ACM certificate %q has no stable identity", certificateARN)
	}
	return out.Certificate, nil
}

func lambdaAPIDomainIntegrationCertificateARNs(ctx context.Context, wildcard string) ([]string, error) {
	var result []string
	var token *string
	seenTokens := map[string]bool{}
	for {
		out, err := AcmClient().ListCertificates(ctx, &acm.ListCertificatesInput{
			CertificateStatuses: []acmtypes.CertificateStatus{
				acmtypes.CertificateStatusPendingValidation,
				acmtypes.CertificateStatusIssued,
			},
			NextToken: token,
		})
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, errors.New("ACM ListCertificates returned nil output")
		}
		for _, summary := range out.CertificateSummaryList {
			if strings.EqualFold(aws.ToString(summary.DomainName), wildcard) &&
				aws.ToString(summary.CertificateArn) != "" {
				result = append(result, aws.ToString(summary.CertificateArn))
			}
		}
		if out.NextToken == nil {
			return result, nil
		}
		nextToken := aws.ToString(out.NextToken)
		if nextToken == "" || seenTokens[nextToken] {
			return nil, errors.New("ACM returned an invalid or repeated certificate pagination cursor")
		}
		seenTokens[nextToken] = true
		token = out.NextToken
	}
}

func lambdaAPIDomainIntegrationValidationRecord(
	certificate *acmtypes.CertificateDetail,
	wildcard string,
) (*acmtypes.ResourceRecord, error) {
	var matches []acmtypes.DomainValidation
	for _, validation := range certificate.DomainValidationOptions {
		if strings.EqualFold(aws.ToString(validation.DomainName), wildcard) {
			matches = append(matches, validation)
		}
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ACM certificate has multiple validation records for %q", wildcard)
	}
	if len(matches) == 0 || matches[0].ResourceRecord == nil {
		return nil, nil
	}
	record := matches[0].ResourceRecord
	if aws.ToString(record.Name) == "" || record.Type != acmtypes.RecordTypeCname ||
		aws.ToString(record.Value) == "" {
		return nil, fmt.Errorf("ACM certificate has an invalid validation record for %q", wildcard)
	}
	return record, nil
}

func lambdaAPIDomainIntegrationEnsureCertificate(
	ctx context.Context,
	zoneName string,
) (string, error) {
	wildcard := "*." + zoneName
	arns, err := lambdaAPIDomainIntegrationCertificateARNs(ctx, wildcard)
	if err != nil {
		return "", err
	}
	if len(arns) > 1 {
		return "", fmt.Errorf("multiple active ACM certificates match %q", wildcard)
	}
	var certificateARN string
	if len(arns) == 1 {
		certificateARN = arns[0]
	} else {
		tokenHash := sha256.Sum256([]byte(wildcard))
		out, err := AcmClient().RequestCertificate(ctx, &acm.RequestCertificateInput{
			DomainName:       aws.String(wildcard),
			IdempotencyToken: aws.String(fmt.Sprintf("%x", tokenHash[:16])),
			ValidationMethod: acmtypes.ValidationMethodDns,
		})
		if err != nil {
			return "", err
		}
		if out == nil || aws.ToString(out.CertificateArn) == "" {
			return "", errors.New("ACM RequestCertificate returned no certificate ARN")
		}
		certificateARN = aws.ToString(out.CertificateArn)
	}
	validationEnsured := false
	for {
		certificate, err := lambdaAPIDomainIntegrationDescribeCertificate(ctx, certificateARN)
		if err != nil {
			return "", err
		}
		switch certificate.Status {
		case acmtypes.CertificateStatusFailed, acmtypes.CertificateStatusValidationTimedOut,
			acmtypes.CertificateStatusRevoked, acmtypes.CertificateStatusExpired:
			return "", fmt.Errorf("ACM certificate %q entered terminal status %s", certificateARN, certificate.Status)
		}
		validationRecord, err := lambdaAPIDomainIntegrationValidationRecord(certificate, wildcard)
		if err != nil {
			return "", err
		}
		if validationRecord != nil && !validationEnsured {
			input := &route53EnsureRecordInput{
				zoneName: zoneName,
				change: &route53types.Change{
					Action: route53types.ChangeActionUpsert,
					ResourceRecordSet: &route53types.ResourceRecordSet{
						Name: validationRecord.Name,
						Type: route53types.RRTypeCname,
						TTL:  aws.Int64(300),
						ResourceRecords: []route53types.ResourceRecord{{
							Value: validationRecord.Value,
						}},
					},
				},
			}
			if err := Route53EnsureRecord(ctx, input, false); err != nil {
				return "", err
			}
			validationEnsured = true
		}
		if certificate.Status == acmtypes.CertificateStatusIssued {
			if !validationEnsured {
				return "", fmt.Errorf("issued ACM certificate %q has no DNS validation record", certificateARN)
			}
			return certificateARN, nil
		}
		if err := lambdaAPIDomainIntegrationWait(ctx, 3*time.Second); err != nil {
			return "", fmt.Errorf("wait for ACM certificate issuance: %w", err)
		}
	}
}

func lambdaAPIDomainIntegrationGetDomain(
	ctx context.Context,
	domainName string,
) (*apigatewayv2.GetDomainNameOutput, error) {
	out, err := ApiClient().GetDomainName(ctx, &apigatewayv2.GetDomainNameInput{
		DomainName: aws.String(domainName),
	})
	if err != nil {
		return nil, err
	}
	if out == nil || aws.ToString(out.DomainName) != domainName || aws.ToString(out.DomainNameArn) == "" {
		return nil, fmt.Errorf("API domain %q has no stable identity", domainName)
	}
	return out, nil
}

func lambdaAPIDomainIntegrationFixtureDomainMatches(
	out *apigatewayv2.GetDomainNameOutput,
	certificateARN string,
) error {
	domain := lambdaAPIDomainFromGet(out)
	if err := lambdaAPIDomainConfigurationError(domain); err != nil {
		return fmt.Errorf("fixture API domain: %w", err)
	}
	configuration := domain.DomainNameConfigurations[0]
	if aws.ToString(configuration.CertificateArn) != certificateARN {
		return fmt.Errorf("fixture API domain certificate = %q, want %q", aws.ToString(configuration.CertificateArn), certificateARN)
	}
	return nil
}

func lambdaAPIDomainIntegrationEnsureFixtureDomain(
	ctx context.Context,
	domainName string,
	certificateARN string,
) error {
	out, err := lambdaAPIDomainIntegrationGetDomain(ctx, domainName)
	var notFound *apitypes.NotFoundException
	if err != nil && !errors.As(err, &notFound) {
		return err
	}
	if errors.As(err, &notFound) {
		for {
			_, err = ApiClient().CreateDomainName(ctx, &apigatewayv2.CreateDomainNameInput{
				DomainName:  aws.String(domainName),
				RoutingMode: apitypes.RoutingModeApiMappingOnly,
				DomainNameConfigurations: []apitypes.DomainNameConfiguration{{
					CertificateArn: aws.String(certificateARN),
					EndpointType:   apitypes.EndpointTypeRegional,
					SecurityPolicy: apitypes.SecurityPolicyTls12,
				}},
			})
			if err == nil {
				break
			}
			var conflict *apitypes.ConflictException
			if errors.As(err, &conflict) {
				break
			}
			var throttled *apitypes.TooManyRequestsException
			if !errors.As(err, &throttled) {
				return err
			}
			if err := lambdaAPIDomainIntegrationWait(ctx, 5*time.Second); err != nil {
				return fmt.Errorf("wait to create fixture API domain: %w", err)
			}
		}
	} else if err := lambdaAPIDomainIntegrationFixtureDomainMatches(out, certificateARN); err != nil {
		return err
	}
	for {
		out, err = lambdaAPIDomainIntegrationGetDomain(ctx, domainName)
		if err != nil {
			return err
		}
		if err := lambdaAPIDomainIntegrationFixtureDomainMatches(out, certificateARN); err != nil {
			return err
		}
		status := out.DomainNameConfigurations[0].DomainNameStatus
		if status == apitypes.DomainNameStatusAvailable {
			return nil
		}
		if status != apitypes.DomainNameStatusUpdating &&
			status != apitypes.DomainNameStatusPendingCertificateReimport &&
			status != apitypes.DomainNameStatusPendingOwnershipVerification {
			return fmt.Errorf("fixture API domain entered status %s", status)
		}
		if err := lambdaAPIDomainIntegrationWait(ctx, 3*time.Second); err != nil {
			return fmt.Errorf("wait for fixture API domain: %w", err)
		}
	}
}

func lambdaAPIDomainIntegrationVerifyValidationRecord(
	t *testing.T,
	ctx context.Context,
	zone route53types.HostedZone,
	certificate *acmtypes.CertificateDetail,
	wildcard string,
) {
	t.Helper()
	validationRecord, err := lambdaAPIDomainIntegrationValidationRecord(certificate, wildcard)
	if err != nil || validationRecord == nil {
		t.Fatalf("ACM validation record = %#v, %v", validationRecord, err)
	}
	var validationStatus acmtypes.DomainStatus
	for _, validation := range certificate.DomainValidationOptions {
		if strings.EqualFold(aws.ToString(validation.DomainName), wildcard) {
			validationStatus = validation.ValidationStatus
		}
	}
	if validationStatus != acmtypes.DomainStatusSuccess {
		t.Fatalf("ACM validation status for %q = %s", wildcard, validationStatus)
	}
	records, err := Route53ListRecords(ctx, aws.ToString(zone.Id))
	if err != nil {
		t.Fatal(err)
	}
	var matching []route53types.ResourceRecordSet
	for _, record := range records {
		if route53DNSNameEqual(aws.ToString(record.Name), aws.ToString(validationRecord.Name)) &&
			record.Type == route53types.RRTypeCname {
			matching = append(matching, record)
		}
	}
	if len(matching) != 1 || !route53RecordIsSimple(&matching[0]) || matching[0].AliasTarget != nil ||
		aws.ToInt64(matching[0].TTL) != 300 || len(matching[0].ResourceRecords) != 1 ||
		!route53DNSNameEqual(aws.ToString(matching[0].ResourceRecords[0].Value), aws.ToString(validationRecord.Value)) {
		t.Fatalf("Route53 ACM validation record = %#v", matching)
	}
}

func lambdaAPIDomainIntegrationVerifyFixture(
	t *testing.T,
	ctx context.Context,
	names lambdaAPIDomainIntegrationNames,
) route53types.HostedZone {
	t.Helper()
	zone := lambdaAPIDomainIntegrationRequireZone(t, ctx, names.zone)
	if nameservers, err := net.LookupNS(names.zone); err != nil || len(nameservers) == 0 {
		t.Fatalf("LIBAWS_TEST_DOMAIN %q is not publicly delegated: %v, %v", names.zone, nameservers, err)
	}
	domain, err := lambdaAPIDomainIntegrationGetDomain(ctx, names.fixtureDomain)
	if err != nil {
		t.Fatal(err)
	}
	if len(domain.Tags) != 0 {
		t.Fatalf("fixture API domain tags = %#v", domain.Tags)
	}
	if len(domain.DomainNameConfigurations) != 1 ||
		domain.DomainNameConfigurations[0].DomainNameStatus != apitypes.DomainNameStatusAvailable {
		t.Fatalf("fixture API domain configurations = %#v", domain.DomainNameConfigurations)
	}
	certificateARN := aws.ToString(domain.DomainNameConfigurations[0].CertificateArn)
	if err := lambdaAPIDomainIntegrationFixtureDomainMatches(domain, certificateARN); err != nil {
		t.Fatal(err)
	}
	mappings, err := lambdaAPIListMappings(ctx, ApiClient(), names.fixtureDomain)
	if err != nil || len(mappings) != 0 {
		t.Fatalf("fixture API mappings = %#v, %v", mappings, err)
	}
	records, err := Route53ListRecords(ctx, aws.ToString(zone.Id))
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if route53DNSNameEqual(aws.ToString(record.Name), names.fixtureDomain) {
			t.Fatalf("fixture API domain unexpectedly has a Route53 record: %#v", record)
		}
	}
	certificate, err := lambdaAPIDomainIntegrationDescribeCertificate(ctx, certificateARN)
	if err != nil {
		t.Fatal(err)
	}
	wildcard := "*." + names.zone
	certificateNames := append([]string{aws.ToString(certificate.DomainName)}, certificate.SubjectAlternativeNames...)
	wildcardFound := false
	for _, name := range certificateNames {
		if strings.EqualFold(name, wildcard) {
			wildcardFound = true
		}
	}
	if !wildcardFound || certificate.Status != acmtypes.CertificateStatusIssued ||
		certificate.RenewalEligibility != acmtypes.RenewalEligibilityEligible {
		t.Fatalf("fixture ACM certificate = %#v", certificate)
	}
	lambdaAPIDomainIntegrationVerifyValidationRecord(t, ctx, zone, certificate, wildcard)
	return zone
}

func lambdaAPIDomainIntegrationEnsureFixture(
	t *testing.T,
	ctx context.Context,
	names lambdaAPIDomainIntegrationNames,
) {
	t.Helper()
	lambdaAPIDomainIntegrationRequireZone(t, ctx, names.zone)
	if nameservers, err := net.LookupNS(names.zone); err != nil || len(nameservers) == 0 {
		t.Fatalf("LIBAWS_TEST_DOMAIN %q is not publicly delegated: %v, %v", names.zone, nameservers, err)
	}
	certificateARN, err := lambdaAPIDomainIntegrationEnsureCertificate(ctx, names.zone)
	if err != nil {
		t.Fatal(err)
	}
	if err := lambdaAPIDomainIntegrationEnsureFixtureDomain(ctx, names.fixtureDomain, certificateARN); err != nil {
		t.Fatal(err)
	}
	for {
		certificate, err := lambdaAPIDomainIntegrationDescribeCertificate(ctx, certificateARN)
		if err != nil {
			t.Fatal(err)
		}
		if certificate.RenewalEligibility == acmtypes.RenewalEligibilityEligible {
			break
		}
		if err := lambdaAPIDomainIntegrationWait(ctx, 3*time.Second); err != nil {
			t.Fatalf("wait for ACM renewal eligibility: %v", err)
		}
	}
	lambdaAPIDomainIntegrationVerifyFixture(t, ctx, names)
}

func lambdaAPIDomainIntegrationVerifyDomain(
	t *testing.T,
	ctx context.Context,
	domainName string,
	infraSetName string,
	api *apitypes.Api,
	zoneID string,
) *apitypes.DomainName {
	t.Helper()
	out, err := lambdaAPIDomainIntegrationGetDomain(ctx, domainName)
	if err != nil {
		t.Fatal(err)
	}
	wantTags := map[string]string{
		infraSetTagName:             infraSetName,
		lambdaAPIDomainAPIIDTagName: aws.ToString(api.ApiId),
	}
	if zoneID != "" {
		wantTags[lambdaAPIDomainRoute53ZoneTagName] = zoneID
	}
	if !reflect.DeepEqual(out.Tags, wantTags) {
		t.Fatalf("API domain %q tags = %#v, want %#v", domainName, out.Tags, wantTags)
	}
	if len(out.DomainNameConfigurations) != 1 ||
		out.DomainNameConfigurations[0].EndpointType != apitypes.EndpointTypeRegional ||
		out.DomainNameConfigurations[0].SecurityPolicy != apitypes.SecurityPolicyTls12 ||
		out.DomainNameConfigurations[0].DomainNameStatus != apitypes.DomainNameStatusAvailable {
		t.Fatalf("API domain %q configurations = %#v", domainName, out.DomainNameConfigurations)
	}
	mappings, err := lambdaAPIListMappings(ctx, ApiClient(), domainName)
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 1 || aws.ToString(mappings[0].ApiId) != aws.ToString(api.ApiId) ||
		aws.ToString(mappings[0].Stage) != lambdaDollarDefault || aws.ToString(mappings[0].ApiMappingKey) != "" {
		t.Fatalf("API mappings for %q = %#v", domainName, mappings)
	}
	return lambdaAPIDomainFromGet(out)
}

func lambdaAPIDomainIntegrationVerifyCreated(
	t *testing.T,
	ctx context.Context,
	names lambdaAPIDomainIntegrationNames,
) {
	t.Helper()
	zone := lambdaAPIDomainIntegrationRequireZone(t, ctx, names.zone)
	httpAPI, err := Api(ctx, names.httpAPI)
	if err != nil || httpAPI == nil || httpAPI.ProtocolType != apitypes.ProtocolTypeHttp {
		t.Fatalf("HTTP API = %#v, %v", httpAPI, err)
	}
	websocketAPI, err := Api(ctx, names.websocketAPI)
	if err != nil || websocketAPI == nil || websocketAPI.ProtocolType != apitypes.ProtocolTypeWebsocket {
		t.Fatalf("WebSocket API = %#v, %v", websocketAPI, err)
	}
	httpDomain := lambdaAPIDomainIntegrationVerifyDomain(
		t, ctx, names.httpDomain, names.infraSet, httpAPI, aws.ToString(zone.Id),
	)
	lambdaAPIDomainIntegrationVerifyDomain(
		t, ctx, names.websocketDomain, names.infraSet, websocketAPI, "",
	)
	records, err := Route53ListRecords(ctx, aws.ToString(zone.Id))
	if err != nil {
		t.Fatal(err)
	}
	httpMatches := 0
	for index := range records {
		switch {
		case route53DNSNameEqual(aws.ToString(records[index].Name), names.httpDomain):
			if !lambdaAPIDNSRecordMatches(httpDomain, &records[index]) {
				t.Fatalf("unexpected HTTP API domain record: %#v", records[index])
			}
			httpMatches++
		case route53DNSNameEqual(aws.ToString(records[index].Name), names.websocketDomain):
			t.Fatalf("domain-only WebSocket API unexpectedly has a Route53 record: %#v", records[index])
		default:
			continue
		}
	}
	if httpMatches != 1 {
		t.Fatalf("matching Route53 aliases for %q = %d", names.httpDomain, httpMatches)
	}
}

func lambdaAPIDomainIntegrationVerifySimpleRecord(
	t *testing.T,
	ctx context.Context,
	names lambdaAPIDomainIntegrationNames,
) {
	t.Helper()
	zone := lambdaAPIDomainIntegrationRequireZone(t, ctx, names.zone)
	records, err := Route53ListRecords(ctx, aws.ToString(zone.Id))
	if err != nil {
		t.Fatal(err)
	}
	var matching []route53types.ResourceRecordSet
	for index := range records {
		if route53DNSNameEqual(aws.ToString(records[index].Name), names.httpDomain) {
			matching = append(matching, records[index])
		}
	}
	if len(matching) != 1 || matching[0].Type != route53types.RRTypeA || matching[0].AliasTarget != nil ||
		aws.ToInt64(matching[0].TTL) != 60 || len(matching[0].ResourceRecords) != 1 ||
		aws.ToString(matching[0].ResourceRecords[0].Value) != "192.0.2.1" {
		t.Fatalf("ordinary Route53 test record for %q = %#v", names.httpDomain, matching)
	}
}

func lambdaAPIDomainIntegrationWaitForDomainRemoval(t *testing.T, ctx context.Context, domainName string) {
	t.Helper()
	for {
		_, err := lambdaAPIDomainIntegrationGetDomain(ctx, domainName)
		var notFound *apitypes.NotFoundException
		if errors.As(err, &notFound) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := lambdaAPIDomainIntegrationWait(ctx, 2*time.Second); err != nil {
			t.Fatalf("wait for API domain %q removal: %v", domainName, err)
		}
	}
}

func lambdaAPIDomainIntegrationVerifyRemoved(
	t *testing.T,
	ctx context.Context,
	names lambdaAPIDomainIntegrationNames,
) {
	t.Helper()
	for _, domainName := range []string{names.httpDomain, names.websocketDomain} {
		lambdaAPIDomainIntegrationWaitForDomainRemoval(t, ctx, domainName)
	}
	for _, apiName := range []string{names.httpAPI, names.websocketAPI} {
		if api, err := Api(ctx, apiName); err == nil || err.Error() != ErrApiNotFound {
			t.Fatalf("API %q still exists or lookup failed unexpectedly: %#v, %v", apiName, api, err)
		}
	}
	zone := lambdaAPIDomainIntegrationRequireZone(t, ctx, names.zone)
	records, err := Route53ListRecords(ctx, aws.ToString(zone.Id))
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if route53DNSNameEqual(aws.ToString(record.Name), names.httpDomain) ||
			route53DNSNameEqual(aws.ToString(record.Name), names.websocketDomain) {
			t.Fatalf("Route53 record remained after infra-rm: %#v", record)
		}
	}
}

func lambdaAPIDomainIntegrationCleanupDomain(
	t *testing.T,
	ctx context.Context,
	domainName string,
	infraSetName string,
) {
	t.Helper()
	out, err := lambdaAPIDomainIntegrationGetDomain(ctx, domainName)
	var domainNotFound *apitypes.NotFoundException
	if err != nil && !errors.As(err, &domainNotFound) {
		t.Error(err)
		return
	}
	if errors.As(err, &domainNotFound) {
		return
	}
	ownerAPIID := out.Tags[lambdaAPIDomainAPIIDTagName]
	if out.Tags[infraSetTagName] != infraSetName || ownerAPIID == "" {
		t.Errorf("refusing to clean API domain %q with ownership tags %#v", domainName, out.Tags)
		return
	}
	mappings, err := lambdaAPIListMappings(ctx, ApiClient(), domainName)
	if err != nil {
		t.Error(err)
		return
	}
	for _, mapping := range mappings {
		if aws.ToString(mapping.ApiId) != ownerAPIID || aws.ToString(mapping.Stage) != lambdaDollarDefault ||
			aws.ToString(mapping.ApiMappingKey) != "" {
			t.Errorf("refusing to clean API domain %q with unexpected mapping %#v", domainName, mapping)
			return
		}
	}
	for _, mapping := range mappings {
		if _, err := ApiClient().DeleteApiMapping(ctx, &apigatewayv2.DeleteApiMappingInput{
			DomainName:   aws.String(domainName),
			ApiMappingId: mapping.ApiMappingId,
		}); err != nil {
			var notFound *apitypes.NotFoundException
			if !errors.As(err, &notFound) {
				t.Error(err)
				return
			}
		}
	}
	for {
		_, err := ApiClient().DeleteDomainName(ctx, &apigatewayv2.DeleteDomainNameInput{
			DomainName: aws.String(domainName),
		})
		var notFound *apitypes.NotFoundException
		if err == nil || errors.As(err, &notFound) {
			return
		}
		var conflict *apitypes.ConflictException
		var throttled *apitypes.TooManyRequestsException
		if !errors.As(err, &conflict) && !errors.As(err, &throttled) {
			t.Error(err)
			return
		}
		if err := lambdaAPIDomainIntegrationWait(ctx, 3*time.Second); err != nil {
			t.Errorf("wait to clean API domain %q: %v", domainName, err)
			return
		}
	}
}

func lambdaAPIDomainIntegrationCleanupAPI(t *testing.T, ctx context.Context, apiName string) {
	t.Helper()
	api, err := Api(ctx, apiName)
	if err != nil {
		if err.Error() != ErrApiNotFound {
			t.Error(err)
		}
		return
	}
	if _, err := ApiClient().DeleteApi(ctx, &apigatewayv2.DeleteApiInput{ApiId: api.ApiId}); err != nil {
		var notFound *apitypes.NotFoundException
		if !errors.As(err, &notFound) {
			t.Error(err)
		}
	}
}

func lambdaAPIDomainIntegrationCleanupDNS(
	t *testing.T,
	ctx context.Context,
	zoneName string,
	domainNames ...string,
) {
	t.Helper()
	zone, err := lambdaAPIDomainIntegrationZone(ctx, zoneName)
	if err != nil {
		t.Error(err)
		return
	}
	if zone == nil {
		t.Errorf("refusing cleanup because fixture hosted zone %q is missing", zoneName)
		return
	}
	records, err := Route53ListRecords(ctx, aws.ToString(zone.Id))
	if err != nil {
		t.Error(err)
		return
	}
	var changes []route53types.Change
	for _, domainName := range domainNames {
		desired := &route53types.ResourceRecordSet{
			Name: aws.String(domainName),
			Type: route53types.RRTypeA,
		}
		record, err := route53SimpleRecord(records, desired)
		if err != nil {
			t.Error(err)
			continue
		}
		if record != nil {
			changes = append(changes, route53types.Change{
				Action: route53types.ChangeActionDelete, ResourceRecordSet: record,
			})
		}
	}
	if len(changes) != 0 {
		if _, err := Route53Client().ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: zone.Id,
			ChangeBatch:  &route53types.ChangeBatch{Changes: changes},
		}); err != nil {
			t.Error(err)
		}
	}
}

func lambdaAPIDomainIntegrationCleanup(
	t *testing.T,
	ctx context.Context,
	names lambdaAPIDomainIntegrationNames,
) {
	t.Helper()
	lambdaAPIDomainIntegrationCleanupDomain(t, ctx, names.httpDomain, names.infraSet)
	lambdaAPIDomainIntegrationCleanupDomain(t, ctx, names.websocketDomain, names.infraSet)
	lambdaAPIDomainIntegrationCleanupAPI(t, ctx, names.httpAPI)
	lambdaAPIDomainIntegrationCleanupAPI(t, ctx, names.websocketAPI)
	lambdaAPIDomainIntegrationCleanupDNS(t, ctx, names.zone, names.httpDomain, names.websocketDomain)
	for _, functionName := range []string{names.httpFunction, names.websocketFunction} {
		if err := LambdaDelete(ctx, functionName, false); err != nil {
			t.Error(err)
		}
	}
	if !t.Failed() {
		lambdaAPIDomainIntegrationVerifyFixture(t, ctx, names)
	}
}

func TestLambdaAPIDomainIntegration(t *testing.T) {
	if os.Getenv("LIBAWS_INTEGRATION") != "1" {
		t.Skip("set LIBAWS_INTEGRATION=1 to run AWS integration tests")
	}
	names := lambdaAPIDomainIntegrationIdentity(t)
	timeout := 5 * time.Minute
	switch os.Getenv("LIBAWS_API_DOMAIN_TEST_MODE") {
	case "ensure-fixture":
		timeout = 10 * time.Minute
	case "cleanup":
		timeout = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	lambdaAPIDomainIntegrationAccount(t, ctx)
	switch os.Getenv("LIBAWS_API_DOMAIN_TEST_MODE") {
	case "ensure-fixture":
		lambdaAPIDomainIntegrationEnsureFixture(t, ctx, names)
	case "verify-fixture":
		lambdaAPIDomainIntegrationVerifyFixture(t, ctx, names)
	case "verify-created":
		lambdaAPIDomainIntegrationVerifyCreated(t, ctx, names)
	case "verify-simple-record":
		lambdaAPIDomainIntegrationVerifySimpleRecord(t, ctx, names)
	case "verify-removed":
		lambdaAPIDomainIntegrationVerifyRemoved(t, ctx, names)
	case "cleanup":
		lambdaAPIDomainIntegrationCleanup(t, ctx, names)
	default:
		t.Fatalf("unknown LIBAWS_API_DOMAIN_TEST_MODE %q", os.Getenv("LIBAWS_API_DOMAIN_TEST_MODE"))
	}
}
