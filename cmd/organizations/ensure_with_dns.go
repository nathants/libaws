package cliaws

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["organizations-ensure-with-dns"] = organizationsEnsureWithDns
	lib.Args["organizations-ensure-with-dns"] = organizationsEnsureWithDnsArgs{}
}

type organizationsEnsureWithDnsArgs struct {
	Name           string `arg:"positional,required"`
	Email          string `arg:"--email,required"`
	ParentDomain   string `arg:"--parent-domain,required"`
	ChildSubDomain string `arg:"--child-subdomain,required"`
	Preview        bool   `arg:"-p,--preview"`
}

func (organizationsEnsureWithDnsArgs) Description() string {
	return "\nensure a sub account with dns delegation from the parent account\n"
}

func organizationsEnsureWithDns() {
	var args organizationsEnsureWithDnsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	var str string

	name := os.Getenv("AWS_CREDS_NAME")
	if name == "" {
		fmt.Println("fatal: AWS_CREDS_NAME == \"\"")
		fmt.Println("follow aws creds setup instructions at: libaws creds-set -h ")
		os.Exit(1)
	}

	creds, err := lib.Session().Config.Credentials.Get()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if creds.AccessKeyID != os.Getenv("AWS_ACCESS_KEY_ID") {
		lib.Logger.Fatal("the key in use does not match the environment variable key. is aws configured in an additional way beyond environment variables?\n%s != %s", creds.AccessKeyID, os.Getenv("AWS_ACCESS_KEY_ID"))
	}

	account, err := lib.StsAccount(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println("parent account id:", account)
	fmt.Println("parent name:", name)
	fmt.Println("this account will be the parent, and should have an organization enabled at: https://console.aws.amazon.com/organizations/v2/home")

	fmt.Println("hit ENTER to proceed")
	_, _ = fmt.Scanln(&str)
	fmt.Println()

	accountID, err := lib.OrganizationsEnsure(ctx, args.Name, args.Email, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println("child account id:", accountID)

	fmt.Println("open an incognito browser tab")
	fmt.Println("hit ENTER to proceed")
	fmt.Println()

	fmt.Println("setup root login for child account via password reset at https://console.aws.amazon.com/ with email:", args.Email)
	fmt.Println("hit ENTER to proceed")
	_, _ = fmt.Scanln(&str)
	fmt.Println()

	fmt.Println("login to the console of the child account, click username at top right, choose my security credentials, and add access key.")
	fmt.Println("save those credentials locally via: libaws creds-add", args.Name, "$KEY_ID", "$KEY_SECRET", "$REGION")
	fmt.Println("hit ENTER to proceed")
	_, _ = fmt.Scanln(&str)
	fmt.Println()

	fmt.Println("in console, click username at top right, choose my security credentials, and add mfa.")
	fmt.Println("hit ENTER to proceed")
	_, _ = fmt.Scanln(&str)
	fmt.Println()

	nsFile := fmt.Sprintf("/tmp/%s.ns.txt", args.ChildSubDomain)

	fmt.Println("setup the child account route53 via the following commands:")
	fmt.Println("")
	fmt.Printf("(aws-creds-temp %s && libaws route53-ensure-zone %s)\n", args.Name, args.ChildSubDomain)
	fmt.Printf("(aws-creds-temp %s && libaws route53-ensure-record %s foo.%s TTL=7 Type=CNAME Value=bar)\n", args.Name, args.ChildSubDomain, args.ChildSubDomain)
	fmt.Printf("(aws-creds-temp %s && libaws route53-ns %s > %s)\n", args.Name, args.ChildSubDomain, nsFile)

	fmt.Println("hit ENTER to proceed")
	_, _ = fmt.Scanln(&str)
	fmt.Println()

	fmt.Println("setup the parent account route53 via the following commands:")
	fmt.Println("")
	bytes, err := ioutil.ReadFile(nsFile)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	cmd := []string{fmt.Sprintf("libaws route53-ensure-record %s %s TTL=172800 Type=NS", args.ParentDomain, args.ChildSubDomain)}
	for _, line := range strings.Split(string(bytes), "\n") {
		if line != "" {
			cmd = append(cmd, "Value="+line)
		}
	}
	fmt.Println(strings.Join(cmd, " "))

	fmt.Println("test the child account dns with:")
	fmt.Println("")
	fmt.Printf("dig foo.%s CNAME # should be: bar\n", args.ChildSubDomain)
}
