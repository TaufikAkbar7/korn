package main

import (
	"fmt"
	"strconv"
	"strings"
)

func Parse(ex string) (*Cron, error) {
	if len(ex) == 0 {
		return nil, fmt.Errorf("empty expr string")
	}

	fields := strings.Fields(ex)

	// check expr length
	if len(fields) > 2 {
		return nil, fmt.Errorf("invalid expr: needs 2 field (minute, hour)")
	}

	minutes, err := ParseField(fields[0], 0, 59)
	if err != nil {
		return nil, err
	}

	hours, err := ParseField(fields[1], 0, 23)
	if err != nil {
		return nil, err
	}

	return &Cron{
		minMask:  minutes,
		hourMask: uint32(hours),
	}, nil
}

// handle parse field for expression /, -, *, ,
// ex: 1-10/2, */15, 5/15
func ParseField(field string, min, max int) (uint64, error) {
	var mask uint64

	// split into several part based on ','
	parts := strings.Split(field, ",")
	for _, part := range parts {
		// handle part expr '/'
		// ex: 1-10/2 or */15
		step := 1
		if strings.Contains(part, "/") {
			subParts := strings.Split(part, "/")
			part = subParts[0]

			val, err := strconv.Atoi(subParts[1])
			if err != nil || val <= 0 {
				return 0, fmt.Errorf("invalid step")
			}
			step = val
		}

		// handle part wildcard '*' or part range '-'
		start, end := 0, 0
		if part == "*" || part == "@hourly" {
			start = min
			end = max
		} else if strings.Contains(part, "-") {
			// split range
			rangeParts := strings.Split(part, "-")
			str, err := strconv.Atoi(rangeParts[0])
			if err != nil {
				return 0, fmt.Errorf("invalid start range")
			}
			start = str

			ed, err := strconv.Atoi(rangeParts[1])
			if err != nil {
				return 0, fmt.Errorf("invalid end range")
			}
			end = ed
		} else {
			// handle single value
			// ex: 5
			val, err := strconv.Atoi(part)
			if err != nil {
				return 0, fmt.Errorf("invalid single value")
			}
			start = val
			end = val
		}

		// validate range
		if start < min || end > max || start > end {
			return 0, fmt.Errorf("value of out range")
		}

		// set bitmask logic
		mask |= getBits(start, end, step)
	}

	return mask, nil
}

func getBits(min, max, step int) uint64 {
	var mask uint64
	for i := min; i <= max; i += step {
		mask |= (1 << uint(i))
	}

	return mask
}

func (e *Cron) GetEnabledMinutes() []int {
	var result []int
	for i := 0; i < 60; i++ {
		// check if bit is true
		if (e.minMask & (1 << uint(i))) != 0 {
			result = append(result, i)
		}
	}
	return result
}

func (e *Cron) GetEnabledHours() []int {
	var result []int
	for i := 0; i < 24; i++ {
		// check if bit is true
		if (e.hourMask & (1 << uint(i))) != 0 {
			result = append(result, i)
		}
	}
	return result
}
