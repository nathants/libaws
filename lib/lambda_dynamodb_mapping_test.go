package lib

import "testing"

func TestLambdaDynamoDBMappingConfiguredRejectsStaleSameTableStream(t *testing.T) {
	current := "arn:aws:dynamodb:us-west-2:337909772623:table/better-game-auth/stream/current"
	stale := "arn:aws:dynamodb:us-west-2:337909772623:table/better-game-auth/stream/stale"
	other := "arn:aws:dynamodb:us-west-2:337909772623:table/other/stream/current"
	configured := []string{current}
	if !lambdaDynamoDBMappingConfigured(current, configured) {
		t.Fatal("current configured stream was rejected")
	}
	if lambdaDynamoDBMappingConfigured(stale, configured) {
		t.Fatal("stale stream for a configured table was retained")
	}
	if lambdaDynamoDBMappingConfigured(other, configured) {
		t.Fatal("unconfigured table stream was retained")
	}
}
