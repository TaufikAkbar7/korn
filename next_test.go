package main

import (
	"fmt"
	"testing"
	"time"
)

func TestNext(t *testing.T) {
	makeTime := func(h, m int) time.Time {
		return time.Date(2026, 4, 29, h, m, 0, 0, time.Local)
	}

	tests := []struct {
		name     string
		expr     string
		current  time.Time
		expected time.Time
	}{
		{
			name:     "Normal Minute Jump",
			expr:     "*/15 *",
			current:  makeTime(10, 0),
			expected: makeTime(10, 15),
		},
		{
			name:     "Jump from end of hour to next hour",
			expr:     "*/15 *",
			current:  makeTime(10, 45),
			expected: makeTime(11, 0),
		},
		{
			name:     "Specific hour and minute",
			expr:     "5 12",
			current:  makeTime(10, 0),
			expected: makeTime(12, 5),
		},
		{
			name:     "Carry over to next day",
			expr:     "0 10",
			current:  makeTime(10, 30),
			expected: makeTime(10, 0).AddDate(0, 0, 1),
		},
		{
			name:     "Complex step and range",
			expr:     "10-30/10 14-16",
			current:  makeTime(14, 15),
			expected: makeTime(14, 20),
		},
		{
			name:     "Smart jump across multiple hours",
			expr:     "0 */4",
			current:  makeTime(01, 0),
			expected: makeTime(04, 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 1. Parse expression
			expr, err := Parse(tt.expr)
			if err != nil {
				t.Fatalf("Failed to parse expr %s: %v", tt.expr, err)
			}
			fmt.Printf("Minute Mask: %060b\n", expr.minMask)
			fmt.Printf("Hour Mask:   %024b\n", expr.hourMask)
			// 2. Hitung Next
			got := expr.Next(tt.current)

			// 3. Bandingkan
			if !got.Equal(tt.expected) {
				t.Errorf("Next() for expr '%s'\ngot : %s\nwant: %s",
					tt.expr, got.Format("15:04"), tt.expected.Format("15:04"))
			}
		})
	}
}
