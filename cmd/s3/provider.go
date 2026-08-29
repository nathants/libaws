package libaws

import "github.com/nathants/libaws/lib"

const s3AWSOnlyProviderDescription = "provider: AWS S3 only"
const s3R2ProviderDescription = "providers: AWS S3 (default), Cloudflare R2 (--r2)"

func s3CommandDescription(summary string, supportsR2 bool) string {
	provider := s3AWSOnlyProviderDescription
	if supportsR2 {
		provider = s3R2ProviderDescription
	}
	return "\n" + summary + "\n\n" + provider + "\n"
}

func configureS3Provider(useR2 bool) {
	if !useR2 {
		return
	}
	if err := lib.S3UseR2FromEnvironment(); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
