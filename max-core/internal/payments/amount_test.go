package payments

import (
	"math/big"
	"testing"
)

func TestParseAmount(t *testing.T) {
	tests := []struct {
		in       string
		decimals int
		want     string
		wantErr  error
	}{
		{"0.1", 9, "100000000", nil},
		{"1", 18, "1000000000000000000", nil},
		{"1.50", 6, "1500000", nil},
		{".5", 6, "500000", nil},
		{"1000000", 5, "100000000000", nil},
		{"0.000001", 6, "1", nil},
		{"0.0000001", 6, "", errAmountPrecision},
		{"0", 6, "", errAmountZero},
		{"0.000", 6, "", errAmountZero},
		{"-1", 6, "", errAmountFormat},
		{"1e6", 6, "", errAmountFormat},
		{"1.2.3", 6, "", errAmountFormat},
		{"", 6, "", errAmountFormat},
		{"abc", 6, "", errAmountFormat},
		{"1,5", 6, "", errAmountFormat},
		{"999999999999999999999999999999999999999999999999999999999999999999999999999999", 18, "", errAmountTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseAmount(tt.in, tt.decimals)
			if err != tt.wantErr {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got.String() != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		v        string
		decimals int
		want     string
	}{
		{"100000000", 9, "0.1"},
		{"1500000", 6, "1.5"},
		{"1", 6, "0.000001"},
		{"1000000", 6, "1"},
		{"42", 0, "42"},
	}
	for _, tt := range tests {
		v, _ := new(big.Int).SetString(tt.v, 10)
		if got := FormatAmount(v, tt.decimals); got != tt.want {
			t.Errorf("FormatAmount(%s, %d) = %s, want %s", tt.v, tt.decimals, got, tt.want)
		}
	}
}

func TestCanTransition(t *testing.T) {
	allowed := [][2]Status{
		{StatusAwaitingSenderSignature, StatusSubmitted},
		{StatusSubmitted, StatusConfirming},
		{StatusConfirming, StatusCompleted},
		{StatusConfirming, StatusSubmitted},
		{StatusExpired, StatusSubmitted},
	}
	forbidden := [][2]Status{
		{StatusCompleted, StatusFailed},
		{StatusCompleted, StatusSubmitted},
		{StatusAwaitingSenderSignature, StatusCompleted},
		{StatusFailed, StatusCompleted},
		{StatusSubmitted, StatusAwaitingSenderSignature},
	}
	for _, p := range allowed {
		if !CanTransition(p[0], p[1]) {
			t.Errorf("%s -> %s should be allowed", p[0], p[1])
		}
	}
	for _, p := range forbidden {
		if CanTransition(p[0], p[1]) {
			t.Errorf("%s -> %s should be forbidden", p[0], p[1])
		}
	}
}
