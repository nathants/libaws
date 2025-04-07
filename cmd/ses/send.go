package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	sestypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ses-send"] = sesSend
	lib.Args["ses-send"] = sesSendArgs{}
}

type sesSendArgs struct {
	ConfigurationSet string `arg:"-c,--conf-set" help:"configuration set name"`
	From             string `arg:"-f,--from"`
	To               string `arg:"-t,--to"`
	Subject          string `arg:"-s,--subject"`
	Body             string `arg:"-b,--body"`
}

func (sesSendArgs) Description() string {
	return "\n \n"
}

func sesSend() {
	var args sesSendArgs
	arg.MustParse(&args)
	ctx := context.Background()
	_, err := lib.SesClient().SendEmail(ctx, &ses.SendEmailInput{
		ConfigurationSetName: aws.String(args.ConfigurationSet),
		Destination: &sestypes.Destination{
			ToAddresses: []string{
				args.To,
			},
		},
		Message: &sestypes.Message{
			Subject: &sestypes.Content{
				Data: aws.String(args.Subject),
			},
			Body: &sestypes.Body{
				Text: &sestypes.Content{
					Data: aws.String(args.Body),
				},
			},
		},
		Source: aws.String(args.From),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
