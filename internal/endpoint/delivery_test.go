package endpoint

import (
	"encoding/json"
	"testing"
)

func TestResolveDelivery(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"localAddr implies forward", `{"localAddr":"127.0.0.1:3000"}`, DeliveryForward},
		{"explicit in_process", `{"delivery":"in_process"}`, DeliveryInProcess},
		{"listen alias", `{"delivery":"listen"}`, DeliveryInProcess},
		{"explicit forward", `{"delivery":"forward","localAddr":"127.0.0.1:1"}`, DeliveryForward},
		{"empty defaults in_process", `{}`, DeliveryInProcess},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveDelivery(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
