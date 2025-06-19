package lib

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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

func EcrUrl(ctx context.Context) (string, error) {
	account, err := StsAccount(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", account, Region()), nil
}
