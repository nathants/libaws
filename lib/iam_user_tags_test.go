package lib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go/middleware"
	"github.com/gofrs/uuid"
)

type iamUserTagsTransport func(*http.Request) (*http.Response, error)

func (transport iamUserTagsTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestIamUserTagsPagination(t *testing.T) {
	for _, test := range []struct {
		name, first, last string
		wantError         bool
	}{
		{"missing cursor", `<IsTruncated>true</IsTruncated>`, "", true},
		{"empty cursor", `<IsTruncated>true</IsTruncated><Marker/>`, "", true},
		{"repeated cursor", `<IsTruncated>true</IsTruncated><Marker>next</Marker>`, `<IsTruncated>true</IsTruncated><Marker>next</Marker>`, true},
		{"complete", `<IsTruncated>true</IsTruncated><Marker>next</Marker>`, `<IsTruncated>false</IsTruncated>`, false},
		{"terminal cursor ignored", `<IsTruncated>true</IsTruncated><Marker>next</Marker>`, `<IsTruncated>false</IsTruncated><Marker>next</Marker>`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			oldClient := iamClient
			t.Cleanup(func() { iamClient = oldClient })
			iamClient = iam.NewFromConfig(aws.Config{
				Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
				HTTPClient: iamUserTagsTransport(func(request *http.Request) (*http.Response, error) {
					if err := request.ParseForm(); err != nil {
						return nil, err
					}
					if request.Form.Get("Action") != "ListUserTags" || reads == 2 {
						return nil, errors.New("unexpected request or unbounded pagination")
					}
					reads++
					body := test.first
					if reads == 2 {
						if request.Form.Get("Marker") != "next" {
							return nil, errors.New("cursor not forwarded")
						}
						body = `<Tags><member><Key>libaws.infraset</Key><Value>wanted</Value></member></Tags>` + test.last
					}
					body = `<ListUserTagsResponse><ListUserTagsResult>` + body + `</ListUserTagsResult></ListUserTagsResponse>`
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
				}),
			})
			tags, err := iamListUserTags(context.Background(), "fixture")
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "did not advance") || len(tags) != 0 {
					t.Fatalf("incomplete membership accepted: tags=%v reads=%d err=%v", tags, reads, err)
				}
			} else if err != nil || reads != 2 || !(infraListScope{setName: "wanted"}).iamTagsMatch(tags) {
				t.Fatalf("membership lost: tags=%v reads=%d err=%v", tags, reads, err)
			}
		})
	}
}

func TestIamUserTagsPaginationIntegration(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name := "test-tags-" + uuid.Must(uuid.NewV4()).String()
	client := IamClient()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		var absent *iamtypes.NoSuchEntityException
		if _, err := client.DeleteUser(cleanupCtx, &iam.DeleteUserInput{UserName: aws.String(name)}); err != nil && !errors.As(err, &absent) {
			t.Errorf("delete user fixture: %v", err)
		}
		if _, err := client.GetUser(cleanupCtx, &iam.GetUserInput{UserName: aws.String(name)}); !errors.As(err, &absent) {
			t.Errorf("user fixture remains or deletion could not be verified: %v", err)
		}
	})
	if _, err := client.CreateUser(ctx, &iam.CreateUserInput{UserName: aws.String(name), Tags: []iamtypes.Tag{
		{Key: aws.String("a-first"), Value: aws.String("first-page")},
		{Key: aws.String(infraSetTagName), Value: aws.String(name)},
	}}); err != nil {
		t.Fatal(err)
	}
	reads := 0
	options := client.Options()
	options.APIOptions = append(slices.Clone(options.APIOptions), func(stack *middleware.Stack) error {
		return stack.Initialize.Add(middleware.InitializeMiddlewareFunc("force user tag pagination", func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
			if input, ok := in.Parameters.(*iam.ListUserTagsInput); ok {
				if aws.ToString(input.UserName) != name {
					return middleware.InitializeOutput{}, middleware.Metadata{}, fmt.Errorf("unexpected fixture user %s", aws.ToString(input.UserName))
				}
				input.MaxItems = aws.Int32(1)
				reads++
			}
			return next.HandleInitialize(ctx, in)
		}), middleware.Before)
	})
	oldClient := iamClient
	t.Cleanup(func() { iamClient = oldClient })
	iamClient = iam.New(options)
	tags, err := iamListUserTags(ctx, name)
	if err != nil || reads < 2 || !(infraListScope{setName: name}).iamTagsMatch(tags) || len(tags) != 2 {
		t.Fatalf("real paginated user membership: tags=%v reads=%d err=%v", tags, reads, err)
	}
}
