package cliaws

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["lambda-invoke"] = lambdaInvoke
	lib.Args["lambda-invoke"] = lambdaInvokeArgs{}
}

type lambdaInvokeArgs struct {
	Name          string `arg:"positional,required"`
	PayloadFile   string `arg:"-f,--payload-file"`
	PayloadString string `arg:"-s,--payload-string"`
	Event         bool   `arg:"-e,--event"`
}

func (lambdaInvokeArgs) Description() string {
	return "\nlambda invoke\n"
}

func lambdaInvoke() {
	var args lambdaInvokeArgs
	arg.MustParse(&args)
	ctx := context.Background()
	invocationType := lambda.InvocationTypeRequestResponse
	logType := lambda.LogTypeTail
	if args.Event {
		invocationType = lambda.InvocationTypeEvent
		logType = lambda.LogTypeNone
	}
	var payload []byte
	if args.PayloadString != "" {
		payload = []byte(args.PayloadString)
	} else if args.PayloadFile != "" {
		var err error
		payload, err = os.ReadFile(args.PayloadFile)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	out, err := lib.LambdaClient().InvokeWithContext(ctx, &lambda.InvokeInput{
		FunctionName:   aws.String(args.Name),
		InvocationType: aws.String(invocationType),
		LogType:        aws.String(logType),
		Payload:        payload,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if out.LogResult != nil {
		log := *out.LogResult
		data, err := base64.StdEncoding.DecodeString(*out.LogResult)
		if err == nil {
			log = string(data)
		}
		fmt.Fprintln(os.Stderr, log)
	}
	if out.FunctionError != nil {
		fmt.Fprintln(os.Stderr, string(out.Payload))
		os.Exit(1)
	} else {
		fmt.Println(string(out.Payload))
	}
}
