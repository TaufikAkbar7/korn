package main

import (
	"fmt"
	"reflect"
	"testing"
)

func generateNum(val int) []int {
	numbers := make([]int, val)
	for i := 0; i < val; i++ {
		numbers[i] = i
	}
	return numbers
}

func TestParse(t *testing.T) {
	wildcardHour := generateNum(24)
	wildcardMin := generateNum(60)

	tests := []struct {
		expr         string
		expectedMin  []int
		expectedHour []int
	}{
		{"*/20 *", []int{0, 20, 40}, wildcardHour},
		{"1,5 *", []int{1, 5}, wildcardHour},
		{"1-3 *", []int{1, 2, 3}, wildcardHour},
		{"3 *", []int{3}, wildcardHour},
		{"* */10", wildcardMin, []int{0, 10, 20}},
		{"* 1-5/2", wildcardMin, []int{1, 3, 5}},
		{"* 1,5,8", wildcardMin, []int{1, 5, 8}},
		{"* *", wildcardMin, wildcardHour},
		{"0 *", []int{0}, wildcardHour},
		{"0 0", []int{0}, []int{0}},
		{"* 0", wildcardMin, []int{0}},
		{"5 1-10", []int{5}, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{"5 1-10/2", []int{5}, []int{1, 3, 5, 7, 9}},
		{"1-2 10-23/10", []int{1, 2}, []int{10, 20}},
		{"1,10,17,50 2,8,12,18", []int{1, 10, 17, 50}, []int{2, 8, 12, 18}},
		{"*/20 2/10", []int{0, 20, 40}, []int{2}},
		{"* @hourly", wildcardMin, wildcardHour},
	}

	for _, tt := range tests {
		res, err := Parse(tt.expr)
		if err != nil {
			t.Errorf("Parse(%s) error: %v", tt.expr, err)
			continue
		}

		got := res.GetEnabledMinutes()
		if !reflect.DeepEqual(got, tt.expectedMin) {
			t.Errorf("Parse(%s) = %v, should %v", tt.expr, got, tt.expectedMin)
		}

		got2 := res.GetEnabledHours()
		if !reflect.DeepEqual(got2, tt.expectedHour) {
			t.Errorf("Parse(%s) = %v, should %v", tt.expr, got2, tt.expectedHour)
		}
	}
}

func TestParseIgnoreSomeExpr(t *testing.T) {
	wildcardMin := generateNum(60)

	tests := []struct {
		expr         string
		expectedMin  []int
		expectedHour []int
	}{
		{"* 1,5,8/2", wildcardMin, []int{1, 5, 8}},
		{"2,4,6/2 1-2", []int{2, 4, 6}, []int{1, 2}},
		{"10/20 2/10", []int{10}, []int{2}},
		{"2-2/20 5-5/10", []int{2}, []int{5}},
	}

	for _, tt := range tests {
		res, err := Parse(tt.expr)
		if err != nil {
			t.Errorf("Parse(%s) error: %v", tt.expr, err)
			continue
		}

		got := res.GetEnabledMinutes()
		if !reflect.DeepEqual(got, tt.expectedMin) {
			t.Errorf("Parse(%s) = %v, should %v", tt.expr, got, tt.expectedMin)
		}

		got2 := res.GetEnabledHours()
		if !reflect.DeepEqual(got2, tt.expectedHour) {
			t.Errorf("Parse(%s) = %v, should %v", tt.expr, got2, tt.expectedHour)
		}
	}
}

func TestBits(t *testing.T) {
	tests := []struct {
		min, max, step int
		expected       uint64
	}{
		{0, 0, 1, 0x1},
		{1, 1, 1, 0x2},
		{1, 5, 2, 0x2a},
		{1, 4, 2, 0xa},
		{1, 1, 3, 0x2},
		{1, 4, 1, 0x1e},
	}

	for i, c := range tests {
		actual := getBits(c.min, c.max, c.step)
		fmt.Printf("hour mask: %060b | index %d \n", actual, i)
		if c.expected != actual {
			t.Errorf("%d-%d/%d => expected %b, got %b",
				c.min, c.max, c.step, c.expected, actual)
		}
	}
}
