package libaws

import "github.com/nathants/libaws/lib"

func s3CommandDescription(summary string, supportsR2 bool) string {
	return lib.S3CommandDescription(summary, supportsR2)
}

func configureS3Provider(useR2 bool) {
	if !useR2 {
		return
	}
	if err := lib.S3UseR2FromEnvironment(); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
