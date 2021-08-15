package lib

import (
	"context"
	"strings"
)

type Infra struct {
}

func InfraDescribe() (*Infra, error) {
	return nil, nil
}

func InfraEnsureS3(ctx context.Context, buckets []string, preview bool) error {
	for _, bucket := range buckets {
		parts := strings.Split(bucket, " ")
		name := parts[0]
		attrs := parts[1:]
		input, err := S3EnsureInput(name, attrs)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = S3Ensure(ctx, input, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func InfraEnsureDynamoDB(ctx context.Context, dbs []string, preview bool) error {
	for _, db := range dbs {
		parts := strings.Split(db, " ")
		name := parts[0]
		var keys []string
		var attrs []string
		for _, part := range parts[1:] {
			if strings.Contains(part, "=") {
				attrs = append(attrs, part)
			} else {
				keys = append(keys, part)
			}
		}
		input, err := DynamoDBEnsureInput(name, keys, attrs)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = DynamoDBEnsure(ctx, input, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func InfraEnsureSqs(ctx context.Context, queues []string, preview bool) error {
	for _, queue := range queues {
		parts := strings.Split(queue, "/")
		name := parts[0]
		attrs := parts[1:]
		input, err := SQSEnsureInput(name, attrs)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = SQSEnsure(ctx, input, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}
