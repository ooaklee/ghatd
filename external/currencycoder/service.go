// Package currencycoder owns currency definitions, precision and availability.
package currencycoder

import (
	"encoding/json"

	"github.com/ooaklee/ghatd/external/catalogue"
)

type Currency struct {
	catalogue.Entry `bson:",inline"`
	MinorUnit       int `json:"minor_unit" bson:"minor_unit"`
}
type CreateRequest = catalogue.CreateRequest[Currency]
type UpdateRequest = catalogue.UpdateRequest[Currency]
type ChangeRequest = catalogue.ChangeRequest

type Service struct{ *catalogue.Lifecycle[Currency] }

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

func MinorUnit(code string) (int, bool) { value, ok := immutableDefinitions[code]; return value, ok }
