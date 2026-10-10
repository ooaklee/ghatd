// Package timezonecoder owns named timezone availability and date-aware offsets.
package timezonecoder

import (
	"context"
	"time"
	_ "time/tzdata"

	"github.com/ooaklee/ghatd/external/catalogue"
)

// Timezone is a catalogue entry whose code must be a known, loadable IANA zone
// name.
type Timezone struct {
	catalogue.Entry `bson:",inline"`
}

// CreateRequest is the catalogue creation request specialised for Timezone
// entries.
type CreateRequest = catalogue.CreateRequest[Timezone]

// UpdateRequest is the catalogue update request specialised for Timezone
// entries.
type UpdateRequest = catalogue.UpdateRequest[Timezone]

// ChangeRequest is the catalogue's generic change request used for timezone
// administration.
type ChangeRequest = catalogue.ChangeRequest

// Service administers the timezone catalogue through the embedded generic
// lifecycle.
type Service struct{ *catalogue.Lifecycle[Timezone] }

// NewService builds a timezone catalogue service whose payload validator
// rejects codes that are unknown to package definitions or cannot be loaded as
// time locations.
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

// knownZone reports whether the code exists in the package's immutable timezone
// definitions.
func knownZone(code string) bool { return definitions[code] }

// Description attaches resolved UTC offset and local time at a specific instant
// to a timezone record already read through this service.
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
