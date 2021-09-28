package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ecs"
)

var ecsClient *ecs.ECS
var ecsClientLock sync.RWMutex
var ecsClientsRegional = make(map[string]*ecs.ECS)

func ECSClient() *ecs.ECS {
	ecsClientLock.Lock()
	defer ecsClientLock.Unlock()
	if ecsClient == nil {
		ecsClient = ecs.New(Session())
	}
	return ecsClient
}
