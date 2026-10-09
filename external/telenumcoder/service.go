// Package telenumcoder owns phone-country definitions, parsing and the
// reachability-provider port. Country calling codes are not residency evidence.
package telenumcoder

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nyaruka/phonenumbers"
	"github.com/ooaklee/ghatd/external/catalogue"
)

type Country struct {
	catalogue.Entry `bson:",inline"`
	CallingCode     string   `json:"calling_code" bson:"calling_code"`
	DialPrefixes    []string `json:"dial_prefixes" bson:"dial_prefixes"`
}
type CreateRequest = catalogue.CreateRequest[Country]
type UpdateRequest = catalogue.UpdateRequest[Country]
type ChangeRequest = catalogue.ChangeRequest
type State string

const (
	Reachable   State = "reachable"
	Unreachable State = "unreachable"
	Unavailable State = "unavailable"
)

var ErrInvalidNumber = errors.New("telenumcoder/invalid-number")
var ErrLookupUnavailable = errors.New("telenumcoder/lookup-unavailable")

type Reachability interface {
	Check(context.Context, string) (State, error)
}
type Option func(*Service)

func WithReachability(provider Reachability) Option {
	return func(s *Service) { s.provider = provider }
}

type Service struct {
	*catalogue.Lifecycle[Country]
	provider Reachability
}

func NewService(repo Repository, clock catalogue.Clock, options ...Option) (*Service, error) {
	lifecycle, err := catalogue.NewLifecycle(repo, clock, func(c *Country) *catalogue.Entry { return &c.Entry }, func(c Country) error {
		expected, ok := definitions[c.Code]
		if !ok || c.CallingCode != "+"+strconv.Itoa(phonenumbers.GetCountryCodeForRegion(c.Code)) {
			return catalogue.ErrInvalidPayload
		}
		if !slices.Equal(c.DialPrefixes, expected.DialPrefixes) {
			return catalogue.ErrInvalidPayload
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	service := &Service{Lifecycle: lifecycle}
	for _, option := range options {
		option(service)
	}
	return service, nil
}

var definitions = func() map[string]Country {
	rows, err := Seeds()
	if err != nil {
		panic("invalid embedded phone-country definitions")
	}
	result := map[string]Country{}
	for _, row := range rows {
		result[row.Code] = row
	}
	return result
}()

// Definition returns a copy of immutable numbering metadata for administration.
func Definition(code string) (Country, bool) {
	value, ok := definitions[code]
	value.DialPrefixes = slices.Clone(value.DialPrefixes)
	value.RegionCodes = slices.Clone(value.RegionCodes)
	return value, ok
}

var phoneCharacters = regexp.MustCompile(`^[+0-9 ().-]+$`)

type Number struct {
	NormalisedNumber string `json:"normalised_number"`
	RegionCode       string `json:"region_code"`
}
type CheckResult struct {
	Number
	State   State  `json:"state"`
	Channel string `json:"channel"`
}

// Normalise parses national input using the selected region. International
// input selects its actual region and can never bypass that region's policy.
func (s *Service) Normalise(ctx context.Context, phone, region string) (Number, error) {
	if err := ctx.Err(); err != nil {
		return Number{}, err
	}
	phone = strings.TrimSpace(phone)
	if len(phone) > 64 || !phoneCharacters.MatchString(phone) {
		return Number{}, ErrInvalidNumber
	}
	if strings.HasPrefix(phone, "+") {
		region = "ZZ"
	} else {
		if _, err := s.RequireSelectable(ctx, region); err != nil {
			return Number{}, err
		}
	}
	parsed, err := phonenumbers.Parse(phone, region)
	if err != nil || !phonenumbers.IsValidNumber(parsed) || parsed.GetExtension() != "" {
		return Number{}, ErrInvalidNumber
	}
	actual := phonenumbers.GetRegionCodeForNumber(parsed)
	if _, err := s.RequireSelectable(ctx, actual); err != nil {
		return Number{}, err
	}
	return Number{NormalisedNumber: phonenumbers.Format(parsed, phonenumbers.E164), RegionCode: actual}, nil
}
func (s *Service) Check(ctx context.Context, phone, region string) (CheckResult, error) {
	number, err := s.Normalise(ctx, phone, region)
	if err != nil {
		return CheckResult{}, err
	}
	result := CheckResult{Number: number, State: Unavailable, Channel: "sms"}
	if s.provider == nil {
		return result, nil
	}
	state, err := s.provider.Check(ctx, number.NormalisedNumber)
	if ctx.Err() != nil {
		return CheckResult{}, ctx.Err()
	}
	if err != nil {
		return result, nil
	}
	if state == Reachable || state == Unreachable || state == Unavailable {
		result.State = state
	}
	return result, nil
}
