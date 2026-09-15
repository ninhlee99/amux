package usage

import "strings"

// FormatTokens renders a compact token count: 0, 850, 12k, 200k, 1M.
func FormatTokens(n int) string {
	if n < 0 {
		n = 0
	}
	switch {
	case n >= 1_000_000:
		if n%1_000_000 == 0 {
			return itoa(n/1_000_000) + "M"
		}
		return trimFloat(float64(n)/1_000_000) + "M"
	case n >= 10_000:
		return itoa(n/1000) + "k"
	case n >= 1000:
		return trimFloat(float64(n)/1000) + "k"
	default:
		return itoa(n)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func trimFloat(v float64) string {
	s := strings.TrimRight(strings.TrimRight(sprintf1(v), "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

func sprintf1(v float64) string {
	if v < 0 {
		return "-" + sprintf1(-v)
	}
	i := int(v)
	frac := int((v-float64(i))*10 + 0.5)
	if frac >= 10 {
		i++
		frac = 0
	}
	if frac == 0 {
		return itoa(i)
	}
	return itoa(i) + "." + itoa(frac)
}
