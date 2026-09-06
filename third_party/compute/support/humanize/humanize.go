// Package humanize provides human-readable representations of numbers, bytes, durations, speed (units/sec), etc.
package humanize

import (
	"fmt"
	"math"
	"time"
)

// EraseToEndOfLine is the ANSI control sequence to erase the current line from the cursor to the end.
const EraseToEndOfLine = "\033[K"

// Bytes returns the rendering of bytes aproximated to the nearest power of 1024 (Kb, Mb, Gb, Tb, etc.)
// with one decimal place.
func Bytes[T ~int64 | ~uint64 | ~int | ~uint | ~int32 | ~uint32 | ~int16 | ~uint16 | ~int8 | ~uint8 | ~uintptr](numAny T) string {
	num := int64(numAny)
	sign := ""
	if num < 0 {
		sign = "-"
		num = -num
	}
	const unit = 1024
	if num < unit {
		return fmt.Sprintf("%s%d B", sign, num)
	}
	div, exp := int64(unit), 0
	for n := num / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%s%.1f %ciB", sign, float64(num)/float64(div), "KMGTPE"[exp])
}

// Underscores returns the rendering of an integer with underscores as thousands separators.
func Underscores[T ~int64 | ~uint64 | ~int | ~uint | ~int32 | ~uint32 | ~int16 | ~uint16 | ~int8 | ~uint8 | ~uintptr](numAny T) string {
	s := fmt.Sprintf("%d", numAny)
	start := 0
	if s[0] == '-' {
		start = 1
	}
	var result []byte
	if start == 1 {
		result = append(result, '-')
	}
	for i := start; i < len(s); i++ {
		if i > start && (len(s)-i)%3 == 0 {
			result = append(result, '_')
		}
		result = append(result, s[i])
	}
	return string(result)
}

// Count returns a compact string representation of an integer count appending powers of 1000 suffixes (K, M, G, T, P, E).
func Count[T ~int64 | ~uint64 | ~int | ~uint | ~int32 | ~uint32 | ~int16 | ~uint16 | ~int8 | ~uint8 | ~uintptr](numT T) string {
	num := int64(numT)
	sign := ""
	if num < 0 {
		sign = "-"
		num = -num
	}
	const unit = 1000
	if num < unit {
		return fmt.Sprintf("%s%d", sign, num)
	}
	div, exp := int64(unit), 0
	for n := num / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	res := fmt.Sprintf("%s%.1f%c", sign, float64(num)/float64(div), "KMGTPE"[exp])
	if len(res) > 3 && res[len(res)-3:len(res)-1] == ".0" {
		res = res[:len(res)-3] + res[len(res)-1:]
	}
	return res
}

// Speed returns some human readable speed (or ratio) of some unit / second.
func Speed[T ~float64 | ~float32](ratioT T, unit string) string {
	ratio := float64(ratioT)
	if ratio > 10 {
		return fmt.Sprintf("%s %s/s", Count(int64(math.Round(ratio))), unit)
	}
	return fmt.Sprintf("%.2f %s/s", ratio, unit)
}

// Duration pretty prints duration without a long list of decimal points.
func Duration(d time.Duration) string {
	if d < 0 {
		return "-" + Duration(-d)
	}

	const day = 24 * time.Hour

	if d >= day {
		days := d / day
		hours := (d % day) / time.Hour
		if hours == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	if d >= time.Hour {
		hours := d / time.Hour
		minutes := (d % time.Hour) / time.Minute
		if minutes == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if d >= time.Minute {
		minutes := d / time.Minute
		seconds := (d % time.Minute) / time.Second
		if seconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}

	var val float64
	var suffix string

	switch {
	case d >= time.Second:
		val = float64(d) / float64(time.Second)
		suffix = "s"
	case d >= time.Millisecond:
		val = float64(d) / float64(time.Millisecond)
		suffix = "ms"
	case d >= time.Microsecond:
		val = float64(d) / float64(time.Microsecond)
		suffix = "µs"
	default:
		return fmt.Sprintf("%dns", d)
	}

	res := fmt.Sprintf("%.1f", val)
	if len(res) >= 2 && res[len(res)-2:] == ".0" {
		res = res[:len(res)-2]
	}
	return res + suffix
}
