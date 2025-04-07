package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["api-ls-domains"] = apiLsDomains
	lib.Args["api-ls-domains"] = apiLsDomainsArgs{}
}

type apiLsDomainsArgs struct {
}

func (apiLsDomainsArgs) Description() string {
	return "\nlist api custom domains\n"
}

func apiLsDomains() {
	var args apiLsDomainsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	domains, err := lib.ApiListDomains(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, domain := range domains {
		api := ""
		mappings, err := lib.ApiClient().GetApiMappings(ctx, &apigatewayv2.GetApiMappingsInput{
			DomainName: domain.DomainName,
			MaxResults: aws.String(fmt.Sprint(500)),
		})
		if err != nil || len(mappings.Items) == 500 {
			lib.Logger.Fatal("error: ", err)
		}
		for _, mapping := range mappings.Items {
			if *mapping.Stage == "$default" {
				out, err := lib.ApiClient().GetApi(ctx, &apigatewayv2.GetApiInput{
					ApiId: mapping.ApiId,
				})
				if err != nil {
					lib.Logger.Fatal("error: ", err)
				}
				api = "api=" + *out.Name
			}
		}
		fmt.Println(*domain.DomainName, api)
	}
}
