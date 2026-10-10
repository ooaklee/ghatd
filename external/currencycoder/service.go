// Package currencycoder owns currency definitions, precision and availability.
package currencycoder

import (
	"encoding/json"

	"github.com/ooaklee/ghatd/external/catalogue"
)

// Currency is a catalogue currency record: an inlined shared entry plus the
// immutable minor-unit exponent for the code.
type Currency struct {
	catalogue.Entry `bson:",inline"`
	MinorUnit       int `json:"minor_unit" bson:"minor_unit"`
}

// CreateRequest aliases the catalogue creation request specialised to Currency
// records.
type CreateRequest = catalogue.CreateRequest[Currency]

// UpdateRequest aliases the catalogue revision-guarded update request
// specialised to Currency records.
type UpdateRequest = catalogue.UpdateRequest[Currency]

// ChangeRequest aliases the catalogue code-and-revision selection used for
// delete and restore transitions.
type ChangeRequest = catalogue.ChangeRequest

// Service embeds the catalogue lifecycle specialised to Currency records,
// exposing shared audit and availability rules.
type Service struct{ *catalogue.Lifecycle[Currency] }

// NewService builds the currency lifecycle, rejecting records whose minor unit
// disagrees with the immutable definition for their code.
func NewService(repo Repository, clock catalogue.Clock) (*Service, error) {
	lifecycle, err := catalogue.NewLifecycle(repo, clock, func(c *Currency) *catalogue.Entry { return &c.Entry }, func(c Currency) error {
		digits, ok := MinorUnit(c.Code)
		if !ok || digits != c.MinorUnit {
			return catalogue.ErrInvalidPayload
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Service{Lifecycle: lifecycle}, nil
}

// immutableDefinitions is accounting metadata, independent of current policy.
// Historic agreements keep their precision when an administrator disables a code.
var immutableDefinitions = func() map[string]int {
	var rows []Currency
	if err := json.Unmarshal(seedData, &rows); err != nil {
		panic("invalid embedded currency definitions")
	}
	result := make(map[string]int, len(rows))
	for _, row := range rows {
		result[row.Code] = row.MinorUnit
	}
	return result
}()

// MinorUnit returns the immutable minor-unit exponent defined for an ISO
// currency code, reporting false for unknown codes.
func MinorUnit(code string) (int, bool) { value, ok := immutableDefinitions[code]; return value, ok }
