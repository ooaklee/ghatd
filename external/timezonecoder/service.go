// Package timezonecoder owns named timezone availability and date-aware offsets.
package timezonecoder

import (
	"context"
	"time"
	_ "time/tzdata"

	"github.com/ooaklee/ghatd/external/catalogue"
)

type Timezone struct {
	catalogue.Entry `bson:",inline"`
}
type CreateRequest = catalogue.CreateRequest[Timezone]
type UpdateRequest = catalogue.UpdateRequest[Timezone]
type ChangeRequest = catalogue.ChangeRequest
type Service struct{ *catalogue.Lifecycle[Timezone] }

func NewService(repo Repository, clock catalogue.Clock) (*Service, error) {
	lifecycle, err := catalogue.NewLifecycle(repo, clock, func(t *Timezone) *catalogue.Entry { return &t.Entry }, func(t Timezone) error {
		if !knownZone(t.Code) {
			return catalogue.ErrInvalidPayload
		}
		_, err := time.LoadLocation(t.Code)
		if err != nil {
			return catalogue.ErrInvalidPayload
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Service{Lifecycle: lifecycle}, nil
}

var definitions = func() map[string]bool {
	rows, err := Seeds()
	if err != nil {
		panic("invalid embedded timezone definitions")
	}
	result := map[string]bool{}
	for _, row := range rows {
		result[row.Code] = true
	}
	return result
}()

func knownZone(code string) bool { return definitions[code] }

type Description struct {
	Record        Timezone
	OffsetSeconds int
	LocalTime     time.Time
}

// Describe uses an explicit instant, including DST rules for that date.
func (s *Service) Describe(ctx context.Context, code string, at time.Time) (Description, error) {
	record, err := s.Get(ctx, code)
	if err != nil {
		return Description{}, err
	}
	return s.DescribeRecord(record, at)
}

// DescribeRecord computes offsets for a record already read through this service.
func (s *Service) DescribeRecord(record Timezone, at time.Time) (Description, error) {
	location, err := time.LoadLocation(record.Code)
	if err != nil {
		return Description{}, err
	}
	local := at.In(location)
	_, offset := local.Zone()
	return Description{Record: record, OffsetSeconds: offset, LocalTime: local}, nil
}
