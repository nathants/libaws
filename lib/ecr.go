package lib

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ecr"
)

var ecrClient *ecr.ECR
var ecrClientLock sync.RWMutex

func EcrClient() *ecr.ECR {
	ecrClientLock.Lock()
	defer ecrClientLock.Unlock()
	if ecrClient == nil {
		ecrClient = ecr.New(Session())
	}
	return ecrClient
}

func EcrDescribeRepos(ctx context.Context) ([]*ecr.Repository, error) {
	var repos []*ecr.Repository
	var token *string
	for {
		out, err := EcrClient().DescribeRepositoriesWithContext(ctx, &ecr.DescribeRepositoriesInput{
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

var ecrEncryptionConfig = &ecr.EncryptionConfiguration{
	EncryptionType: aws.String("AES256"),
}

func EcrEnsure(ctx context.Context, name string, preview bool) error {
	out, err := EcrClient().DescribeRepositoriesWithContext(ctx, &ecr.DescribeRepositoriesInput{
		RepositoryNames: []*string{aws.String(name)},
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != ecr.ErrCodeRepositoryNotFoundException {
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			_, err := EcrClient().CreateRepositoryWithContext(ctx, &ecr.CreateRepositoryInput{
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
