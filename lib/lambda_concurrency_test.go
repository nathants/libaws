package lib

import "testing"

func TestLambdaConcurrencyNeedsUpdateDistinguishesReservedZero(t *testing.T) {
	zero := int32(0)
	four := int32(4)
	five := int32(5)

	for name, test := range map[string]struct {
		current *int32
		desired int
		want    bool
	}{
		"unreserved remains unreserved":     {current: nil, desired: 0, want: false},
		"reserved zero becomes unreserved":  {current: &zero, desired: 0, want: true},
		"reserved value becomes unreserved": {current: &five, desired: 0, want: true},
		"unreserved becomes reserved":       {current: nil, desired: 5, want: true},
		"matching reservation remains":      {current: &five, desired: 5, want: false},
		"different reservation changes":     {current: &four, desired: 5, want: true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := lambdaConcurrencyNeedsUpdate(test.current, test.desired); got != test.want {
				t.Fatalf("lambdaConcurrencyNeedsUpdate(%v, %d) = %t, want %t", test.current, test.desired, got, test.want)
			}
		})
	}
}
