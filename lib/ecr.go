package lib

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

var ecrClient *ecr.Client
var ecrClientLock sync.Mutex

func EcrClientExplicit(accessKeyID, accessKeySecret, region string) *ecr.Client {
	return ecr.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func EcrClient() *ecr.Client {
	ecrClientLock.Lock()
	defer ecrClientLock.Unlock()
	if ecrClient == nil {
		ecrClient = ecr.NewFromConfig(*Session())
	}
	return ecrClient
}

func EcrDescribeRepos(ctx context.Context) ([]ecrtypes.Repository, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EcrDescribeRepos"}
		d.Start()
		defer d.End()
	}
	var repos []ecrtypes.Repository
	var token *string
	for {
		out, err := EcrClient().DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		repos = append(repos, out.Repositories...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return repos, nil
}

var ecrEncryptionConfig = &ecrtypes.EncryptionConfiguration{
	EncryptionType: ecrtypes.EncryptionTypeAes256,
}

func EcrEnsure(ctx context.Context, name string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EcrEnsure"}
		d.Start()
		defer d.End()
	}
	out, err := EcrClient().DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{
		RepositoryNames: []string{name},
	})
	if err != nil {
		var notFound *ecrtypes.RepositoryNotFoundException
		if !errors.As(err, &notFound) {
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			_, err := EcrClient().CreateRepository(ctx, &ecr.CreateRepositoryInput{
				EncryptionConfiguration: ecrEncryptionConfig,
				RepositoryName:          aws.String(name),
			})
			if err != nil {
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"ecr created repo:", name)
		return nil
	}
	if len(out.Repositories) != 1 {
		panic(len(out.Repositories))
	}
	if !reflect.DeepEqual(out.Repositories[0].EncryptionConfiguration, ecrEncryptionConfig) {
		err := fmt.Errorf("ecr repo is misconfigured: %s", name)
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func ecrLoginCommand(ctx context.Context, output *ecr.GetAuthorizationTokenOutput) (*exec.Cmd, error) {
	if output == nil || len(output.AuthorizationData) != 1 {
		return nil, errors.New("ECR returned invalid authorization data")
	}
	authorization := output.AuthorizationData[0]
	if authorization.AuthorizationToken == nil || authorization.ProxyEndpoint == nil || strings.TrimSpace(*authorization.ProxyEndpoint) == "" {
		return nil, errors.New("ECR returned incomplete authorization data")
	}
	credentials, err := base64.StdEncoding.DecodeString(*authorization.AuthorizationToken)
	if err != nil {
		return nil, fmt.Errorf("decode ECR authorization token: %w", err)
	}
	username, password, ok := strings.Cut(string(credentials), ":")
	if !ok || username == "" || password == "" {
		return nil, errors.New("invalid ECR authorization token")
	}
	command := exec.CommandContext(ctx, "docker", "login", "--username", username, "--password-stdin", *authorization.ProxyEndpoint)
	command.Stdin = strings.NewReader(password)
	return command, nil
}

// EcrLogin authenticates the current Docker configuration without putting the password in process arguments.
func EcrLogin(ctx context.Context, stdout, stderr io.Writer) error {
	output, err := EcrClient().GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return err
	}
	command, err := ecrLoginCommand(ctx, output)
	if err != nil {
		return err
	}
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func EcrUrl(ctx context.Context) (string, error) {
	account, err := StsAccount(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", account, Region()), nil
}
