package libaws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["s3-head"] = s3Head
	lib.Args["s3-head"] = s3HeadArgs{}
}

type s3HeadArgs struct {
	Path string `arg:"positional"`
	R2   bool   `arg:"--r2" help:"use Cloudflare R2 credentials and endpoint"`
}

func (s3HeadArgs) Description() string {
	return s3CommandDescription("head an object", true)
}

func s3Head() {
	var args s3HeadArgs
	arg.MustParse(&args)
	configureS3Provider(args.R2)
	ctx := context.Background()

	args.Path = strings.ReplaceAll(args.Path, "s3://", "")
	bucket, key, err := lib.SplitOnce(args.Path, "/")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	s3Client, err := lib.S3ClientBucketRegion(ctx, bucket)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	out, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(key),
		ChecksumMode: s3types.ChecksumModeEnabled,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	data, err := json.Marshal(out)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	var outMap map[string]any
	err = json.Unmarshal(data, &outMap)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	val := map[string]any{}
	for k, v := range outMap {
		if v != nil {
			val[k] = v
		}
	}
	fmt.Println(lib.Pformat(val))
}
