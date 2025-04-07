package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["route53-describe-zone"] = route53DescribeZone
	lib.Args["route53-describe-zone"] = route53DescribeZoneArgs{}
}

type route53DescribeZoneArgs struct {
	Name string `arg:"positional,required"`
}

func (route53DescribeZoneArgs) Description() string {
	return "\ndescribe zone\n"
}

func route53DescribeZone() {
	var args route53DescribeZoneArgs
	arg.MustParse(&args)
	ctx := context.Background()
	id, err := lib.Route53ZoneID(ctx, args.Name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	out, err := lib.Route53Client().GetHostedZone(ctx, &route53.GetHostedZoneInput{
		Id: aws.String(id),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(lib.Pformat(out))
}
