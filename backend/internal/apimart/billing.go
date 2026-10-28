package apimart

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// The same release-time table controls the model menu and the server quote.
// Prices are APIMart's public Gold USD prices in units of 0.0000001 USD;
// image rates are per image, video rates are per output second.
//
//go:embed billing_models.json
var billingJSON []byte

type billingRate struct {
	Rates          map[string]int64 `json:"rates"`
	AudioRates     map[string]int64 `json:"audioRates"`
	FreeReferences int              `json:"freeReferences"`
	ReferenceRate  int64            `json:"referenceRate"`
}

var billing = func() struct {
	Version string                 `json:"version"`
	Images  map[string]billingRate `json:"images"`
	Videos  map[string]billingRate `json:"videos"`
} {
	var table struct {
		Version string                 `json:"version"`
		Images  map[string]billingRate `json:"images"`
		Videos  map[string]billingRate `json:"videos"`
	}
	if json.Unmarshal(billingJSON, &table) != nil || table.Version == "" || len(table.Images) == 0 || len(table.Videos) == 0 {
		panic("invalid APIMart billing table")
	}
	return table
}()

func BillingVersion() string { return billing.Version }

func Billable(kind, model string) bool {
	if kind == "image" {
		_, ok := billing.Images[model]
		return ok
	}
	if kind == "video" {
		_, ok := billing.Videos[model]
		return ok
	}
	return false
}

// FixedCredits quotes one image or one video after request validation. The
// platform's established 2*ceil(12*RMB) rule is applied using a fixed
// USD/CNY rate of 7.2. Integer arithmetic avoids rounding money with floats.
func FixedCredits(kind, model, resolution string, duration int, audio bool, references int) (int64, bool) {
	var entry billingRate
	var ok bool
	switch kind {
	case "image":
		entry, ok = billing.Images[model]
	case "video":
		entry, ok = billing.Videos[model]
	default:
		return 0, false
	}
	if !ok || references < 0 || references > 50 || (kind == "video" && (duration <= 0 || duration > 30)) {
		return 0, false
	}
	rates := entry.Rates
	if audio && len(entry.AudioRates) > 0 {
		rates = entry.AudioRates
	}
	rate := rates[strings.ToUpper(resolution)]
	if rate <= 0 {
		return 0, false
	}
	cost := rate
	if kind == "video" {
		cost *= int64(duration)
	}
	cost += int64(max(0, references-entry.FreeReferences)) * entry.ReferenceRate
	const denominator int64 = 100000000
	points := 2 * ((cost*864 + denominator - 1) / denominator)
	return points, points > 0 && points <= 1000000
}
