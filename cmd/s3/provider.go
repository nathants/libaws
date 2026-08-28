package libaws

import "github.com/nathants/libaws/lib"

func configureS3Provider(useR2 bool) {
	if !useR2 {
		return
	}
	if err := lib.S3UseR2FromEnvironment(); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
