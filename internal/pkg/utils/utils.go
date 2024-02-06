package utils

import (
	"fmt"
	"strings"
)

func Float32SliceToString(slice []float32) string {
	strs := make([]string, len(slice))
	for i, v := range slice {
		strs[i] = fmt.Sprintf("%f", v)
	}
	return strings.Join(strs, ",")
}
