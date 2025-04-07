package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["route53-describe-records"] = route53DescribeRecords
	lib.Args["route53-describe-records"] = route53DescribeRecordsArgs{}
}

type route53DescribeRecordsArgs struct {
	Name string `arg:"positional,required"`
}

func (route53DescribeRecordsArgs) Description() string {
	return "\ndescribe records\n"
}

func route53DescribeRecords() {
	var args route53DescribeRecordsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	id, err := lib.Route53ZoneID(ctx, args.Name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	records, err := lib.Route53ListRecords(ctx, id)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(lib.Pformat(records))
}
