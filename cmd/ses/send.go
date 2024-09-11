package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ses"
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
	_, err := lib.SesClient().SendEmailWithContext(ctx, &ses.SendEmailInput{
		ConfigurationSetName: aws.String(args.ConfigurationSet),
		Destination: &ses.Destination{
			ToAddresses: []*string{
				aws.String(args.To),
			},
		},
		Message: &ses.Message{
			Subject: &ses.Content{
				Data: aws.String(args.Subject),
			},
			Body: &ses.Body{
				Text: &ses.Content{
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
