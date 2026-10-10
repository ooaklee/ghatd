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

// Country is a catalogue entry for a calling region; it adds the E.164 calling
// code and dial prefixes to the inline catalogue Entry, validated against
// immutable package definitions.
type Country struct {
	catalogue.Entry `bson:",inline"`
	CallingCode     string   `json:"calling_code" bson:"calling_code"`
	DialPrefixes    []string `json:"dial_prefixes" bson:"dial_prefixes"`
}

// CreateRequest is the catalogue creation request specialised for Country
// entries.
type CreateRequest = catalogue.CreateRequest[Country]

// UpdateRequest is the catalogue update request specialised for Country
// entries.
type UpdateRequest = catalogue.UpdateRequest[Country]

// ChangeRequest is the catalogue's generic change request used for country
// administration.
type ChangeRequest = catalogue.ChangeRequest

// State is the reachability verdict for a normalised number: Reachable,
// Unreachable or Unavailable.
type State string

const (
	Reachable   State = "reachable"
	Unreachable State = "unreachable"
	Unavailable State = "unavailable"
)

var ErrInvalidNumber = errors.New("telenumcoder/invalid-number")
var ErrLookupUnavailable = errors.New("telenumcoder/lookup-unavailable")

// Reachability checks whether a normalised number can currently receive
// messages; implementations decide what transport and policy back the verdict.
type Reachability interface {
	// Check reports whether the supplied normalised number can currently receive
	// messages as a State; implementations decide the transport and policy behind
	// the verdict.
	Check(context.Context, string) (State, error)
}

// Option configures a Service during construction.
type Option func(*Service)

// WithReachability installs the provider used for SMS-channel reachability
// checks.
func WithReachability(provider Reachability) Option {
	return func(s *Service) { s.provider = provider }
}

// Service administers the country catalogue through the embedded generic
// lifecycle and optionally consults a Reachability provider for number checks.
type Service struct {
	*catalogue.Lifecycle[Country]
	provider Reachability
}

// NewService builds a country catalogue service whose payload validator rejects
// unknown region codes, calling codes inconsistent with libphonenumber's region
// mapping, or dial prefixes differing from package definitions.
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

// Number is a normalised telephone number paired with the region it was
// resolved against.
type Number struct {
	NormalisedNumber string `json:"normalised_number"`
	RegionCode       string `json:"region_code"`
}

// CheckResult embeds the normalised number and adds the reachability state and
// the channel that was checked, defaulting to "sms".
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

// Check normalises the phone number for the region, then queries the configured
// reachability provider. Without a provider, or when the provider errs, it
// returns Unavailable with a nil error; caller cancellation is surfaced as
// ctx.Err().
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
