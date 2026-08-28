package libaws

import "testing"

func TestDynamoDBItemScanLimit(t *testing.T) {
	if err := validateDynamoDBItemScanLimit(-1); err == nil {
		t.Fatal("negative limit should fail")
	}
	for _, limit := range []int{0, 1, 10} {
		if err := validateDynamoDBItemScanLimit(limit); err != nil {
			t.Fatalf("limit %d failed: %v", limit, err)
		}
	}
	if dynamoDBItemScanLimitReached(0, 100) {
		t.Fatal("zero means unlimited")
	}
	if dynamoDBItemScanLimitReached(2, 1) {
		t.Fatal("limit reached too early")
	}
	if !dynamoDBItemScanLimitReached(2, 2) {
		t.Fatal("limit was not reached exactly")
	}
}
