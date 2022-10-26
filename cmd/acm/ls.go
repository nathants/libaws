package cliaws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/acm"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["acm-ls"] = acmLs
	lib.Args["acm-ls"] = acmLsArgs{}
}

type acmLsArgs struct {
}

func (acmLsArgs) Description() string {
	return "\nlist acm certificates\n"
}

func acmLs() {
	var args acmLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	certs, err := lib.AcmListCertificates(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, cert := range certs {
		out, err := lib.AcmClient().DescribeCertificateWithContext(ctx, &acm.DescribeCertificateInput{
			CertificateArn: cert.CertificateArn,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}

		fmt.Println(
			strings.Join(lib.StringSlice(out.Certificate.SubjectAlternativeNames), " "),
			"renewal="+*out.Certificate.RenewalEligibility,
			"status="+*out.Certificate.Status,
			"expires="+out.Certificate.NotAfter.Format(time.RFC3339),
		)
	}
}
