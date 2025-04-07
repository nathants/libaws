package libaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["api-url-domain"] = apiUrlDomain
	lib.Args["api-url-domain"] = apiUrlDomainArgs{}
}

type apiUrlDomainArgs struct {
	Name string `arg:"positional,required"`
}

func (apiUrlDomainArgs) Description() string {
	return "\nget api custom domain url\n"
}

func apiUrlDomain() {
	var args apiUrlDomainArgs
	arg.MustParse(&args)
	ctx := context.Background()
	domain, err := lib.ApiListDomains(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	found := false
	for _, domain := range domain {
		mappings, err := lib.ApiClient().GetApiMappings(ctx, &apigatewayv2.GetApiMappingsInput{
			DomainName: domain.DomainName,
			MaxResults: aws.String(fmt.Sprint(500)),
		})
		if err != nil || len(mappings.Items) == 500 {
			lib.Logger.Fatal("error: ", err)
		}
		for _, mapping := range mappings.Items {
			if *mapping.Stage == "$default" {
				api, err := lib.ApiClient().GetApi(ctx, &apigatewayv2.GetApiInput{
					ApiId: mapping.ApiId,
				})
				if err != nil {
					lib.Logger.Fatal("error: ", err)
				}
				if args.Name == *api.Name || args.Name+lib.LambdaWebsocketSuffix == *api.Name {
					found = true
					if len(domain.DomainNameConfigurations) != 1 {
						panic(lib.PformatAlways(domain))
					}
					fmt.Println(*api.Name, *domain.DomainName, *domain.DomainNameConfigurations[0].ApiGatewayDomainName)
				}
			}
		}
	}
	if !found {
		os.Exit(1)
	}
}
