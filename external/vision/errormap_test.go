package vision

import (
	"strings"
	"testing"
)

func TestVisionErrorMapUsesUniqueVisionCodes(t *testing.T) {
	seen := make(map[string]struct{}, len(VisionErrorMap))
	for err, item := range VisionErrorMap {
		if !strings.HasPrefix(item.Code, "VIS0-") {
			t.Fatalf("error %v code = %q, want VIS0 prefix", err, item.Code)
		}
		if _, exists := seen[item.Code]; exists {
			t.Fatalf("duplicate vision error code %q", item.Code)
		}
		seen[item.Code] = struct{}{}
	}
}
