package scheduler

import "testing"

func TestValidateCron(t *testing.T) {
	ok := []string{
		"* * * * *",
		"0 9 * * *",
		"*/15 * * * *",
		"0 0 1 1 0",
		"30 8-17 * * 1-5",
		"0,30 * * * *",
		"5 4 * * 6",
	}
	for _, expr := range ok {
		if err := ValidateCron(expr); err != nil {
			t.Errorf("ValidateCron(%q) = %v, want nil", expr, err)
		}
	}
	bad := []string{
		"",
		"* * * *",        // too few fields
		"* * * * * *",    // too many
		"60 * * * *",     // minute out of range
		"* 24 * * *",     // hour out of range
		"* * 0 * *",      // day 0
		"* * 32 * *",     // day out of range
		"* * * 13 *",     // month out of range
		"* * * * 7",      // weekday out of range (matcher uses 0-6)
		"abc * * * *",    // not a number
		"*/0 * * * *",    // zero step
		"5-1 * * * *",    // inverted range
		"1-60 * * * *",   // range out of bounds
		"5-10/2 * * * *", // range steps unsupported
		"1,,2 * * * *",   // empty list element
	}
	for _, expr := range bad {
		if err := ValidateCron(expr); err == nil {
			t.Errorf("ValidateCron(%q) = nil, want error", expr)
		}
	}
}
