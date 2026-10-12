package domain

import "slices"

// SelectOptionColors is the authoritative writer palette in OpenAPI order.
var SelectOptionColors = []string{
	"gray", "red", "orange", "yellow", "lime", "green",
	"cyan", "blue", "indigo", "purple", "pink", "brown",
}

func ValidSelectOptionColor(color string) bool {
	return slices.Contains(SelectOptionColors, color)
}
