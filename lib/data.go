package lib

import "os"

const (
	ErrPrefixDidntFindExactlyOne = "didn't find exactly one"
)

var doDebug = os.Getenv("DEBUG") != ""
