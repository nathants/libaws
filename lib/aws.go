package lib

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

var Commands = make(map[string]func())

var ctx = context.Background()

var cfg *aws.Config
var cfgLock sync.RWMutex

func Config() aws.Config {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		_cfg, err := config.LoadDefaultConfig()
		panic1(err)
		cfg = &_cfg
	}
	return *cfg
}
