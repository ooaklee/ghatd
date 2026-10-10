package internationalisationmanager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/currencycoder"
	"github.com/ooaklee/ghatd/external/globalflagger"
	"github.com/ooaklee/ghatd/external/telenumcoder"
	"github.com/ooaklee/ghatd/external/timezonecoder"
)

// Service composes the owning child services. It has no datastore or provider
// access; the host authenticates administrators before invoking commands.
type Service struct {
	currencies *currencycoder.Service
	phones     *telenumcoder.Service
	flags      *globalflagger.Service
	timezones  *timezonecoder.Service
	clock      catalogue.Clock
}

// NewService composes the four owning catalogue services; a nil child service
// returns catalogue.ErrUnavailable and a nil clock defaults to the real clock.
func NewService(currencies *currencycoder.Service, phones *telenumcoder.Service, flags *globalflagger.Service, timezones *timezonecoder.Service, clock catalogue.Clock) (*Service, error) {
	if currencies == nil || phones == nil || flags == nil || timezones == nil {
		return nil, catalogue.ErrUnavailable
	}
	if clock == nil {
		clock = catalogue.RealClock{}
	}
	return &Service{currencies: currencies, phones: phones, flags: flags, timezones: timezones, clock: clock}, nil
}

var _ HTTPService = (*Service)(nil)

// domainError maps known catalogue and coder failures onto transport Error
// codes, and returns other errors unchanged so the HTTP boundary can hide
// native text.
func domainError(err error) error {
	if err == nil {
		return nil
	}
	var known *Error
	if errors.As(err, &known) {
		return err
	}
	switch {
	case errors.Is(err, catalogue.ErrNotFound), errors.Is(err, catalogue.ErrNotSelectable):
		return &Error{Code: "I18N_NOT_FOUND", Status: 404}
	case errors.Is(err, catalogue.ErrStaleWrite):
		return &Error{Code: "I18N_STALE_WRITE", Status: 412}
	case errors.Is(err, catalogue.ErrAlreadyExists):
		return &Error{Code: "I18N_ALREADY_EXISTS", Status: 409}
	case errors.Is(err, globalflagger.ErrSVGTooLarge), errors.Is(err, globalflagger.ErrSVGMalformed), errors.Is(err, globalflagger.ErrSVGUnsafe), errors.Is(err, catalogue.ErrInvalidPayload), errors.Is(err, telenumcoder.ErrInvalidNumber), errors.Is(err, globalflagger.ErrAlreadyDeleted), errors.Is(err, globalflagger.ErrNotDeleted):
		return &Error{Code: "I18N_INVALID_RECORD", Status: 422}
	default:
		return err // HTTP boundary hides native error text.
	}
}

// invalidRecord returns the 422 invalid-record error attributed to a field.
func invalidRecord(field string) error {
	return &Error{Code: "I18N_INVALID_RECORD", Status: 422, Field: field}
}

// entryView projects common catalogue fields; audit actor fields are included
// only for admin views.
func entryView(entry catalogue.Entry, admin bool) RecordView {
	updated := entry.CreatedAt
	if entry.UpdatedAt != nil {
		updated = *entry.UpdatedAt
	}
	result := RecordView{Code: entry.Code, Name: entry.Name, Enabled: entry.Enabled, Hidden: entry.Hidden, Revision: int64(entry.Revision), CreatedAt: entry.CreatedAt, UpdatedAt: updated, DeletedAt: entry.DeletedAt, FlagID: entry.FlagID, RegionCodes: entry.RegionCodes}
	if admin {
		result.CreatedBy = entry.CreatedBy
		result.UpdatedBy = entry.UpdatedBy
		result.DeletedBy = entry.DeletedBy
	}
	return result
}

// currencyView extends the common view with the currency's minor unit.
func currencyView(record currencycoder.Currency, admin bool) RecordView {
	result := entryView(record.Entry, admin)
	result.MinorUnit = &record.MinorUnit
	return result
}

// phoneView extends the common view with calling code and dial prefixes.
func phoneView(record telenumcoder.Country, admin bool) RecordView {
	result := entryView(record.Entry, admin)
	result.CallingCode = record.CallingCode
	result.DialPrefixes = record.DialPrefixes
	return result
}

// timezoneView extends the common view with the offset and local time described
// at the given instant; description failures propagate.
func (s *Service) timezoneView(record timezonecoder.Timezone, admin bool, at time.Time) (RecordView, error) {
	result := entryView(record.Entry, admin)
	description, err := s.timezones.DescribeRecord(record, at)
	if err != nil {
		return RecordView{}, err
	}
	result.OffsetSeconds = &description.OffsetSeconds
	result.LocalTime = description.LocalTime.Format(time.RFC3339)
	return result, nil
}

// flagURL builds the admin or public SVG endpoint URL, cache-busting with the
// record revision.
func flagURL(code string, revision int, admin bool) string {
	prefix := BasePath
	if admin {
		prefix += "/admin"
	}
	return fmt.Sprintf("%s/flags/%s/svg?v=%d", prefix, url.PathEscape(code), revision)
}

// flagView projects a flag record with its SVG URL; the raw SVG is included
// only when requested.
func flagView(record globalflagger.Record, admin, includeSVG bool) RecordView {
	result := entryView(catalogue.Entry{Code: record.Code, Name: record.Name, Enabled: record.Enabled, Hidden: record.Hidden, Audit: record.Audit}, admin)
	result.SVGURL = flagURL(record.Code, record.Revision, admin)
	if includeSVG {
		result.SVG = record.SVG
	}
	return result
}

// isPublic reports whether a record is enabled, unhidden and undeleted, i.e.
// selectable by non-admin callers.
func isPublic(record RecordView) bool {
	return catalogue.Selectable(record.Enabled, record.Hidden, record.DeletedAt)
}

// Cursors are bounded continuation positions, scoped to the list and page size.
// They grant no access: every administrative request is independently admitted.
type cursor struct {
	Kind  Kind `json:"k"`
	Admin bool `json:"a"`
	Page  int  `json:"p"`
	Limit int  `json:"l"`
}

// pageQuery validates kind and limit, and decodes a cursor only if it matches
// the request's kind, admin flag and page size and names a page between 2 and
// 10000; otherwise it returns an invalid request. Admin queries include
// deleted, hidden and disabled records.
func pageQuery(kind Kind, admin bool, q ListQuery) (catalogue.ListQuery, error) {
	if !kind.valid() || q.Limit < 1 || q.Limit > 200 {
		return catalogue.ListQuery{}, invalidHTTP()
	}
	page := 1
	if q.Cursor != "" {
		if len(q.Cursor) > 256 {
			return catalogue.ListQuery{}, invalidHTTP()
		}
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil {
			return catalogue.ListQuery{}, invalidHTTP()
		}
		var c cursor
		if json.Unmarshal(raw, &c) != nil || c.Kind != kind || c.Admin != admin || c.Limit != q.Limit || c.Page < 2 || c.Page > 10000 {
			return catalogue.ListQuery{}, invalidHTTP()
		}
		page = c.Page
	}
	return catalogue.ListQuery{Page: page, PageSize: q.Limit, IncludeDeleted: admin, IncludeHidden: admin, IncludeDisabled: admin}, nil
}

// nextCursor returns an encoded next-page cursor when more pages remain, and an
// empty string on the final page.
func nextCursor(kind Kind, admin bool, q catalogue.ListQuery, total int64) string {
	if int64(q.Page*q.PageSize) >= total {
		return ""
	}
	raw, _ := json.Marshal(cursor{kind, admin, q.Page + 1, q.PageSize})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// List returns one page of the requested kind. Flags use the public listing for
// non-admin callers; other kinds attach flag URLs via a bounded index. Failures
// are mapped through domainError and an empty page is always non-nil.
func (s *Service) List(ctx context.Context, kind Kind, admin bool, query ListQuery) (result ListResult, err error) {
	defer func() { err = domainError(err) }()
	q, err := pageQuery(kind, admin, query)
	if err != nil {
		return result, err
	}
	result.Records = []RecordView{}
	var total int64
	switch kind {
	case Currencies:
		records, count, e := s.currencies.List(ctx, q)
		if e != nil {
			return result, e
		}
		total = count
		for _, r := range records {
			result.Records = append(result.Records, currencyView(r, admin))
		}
	case PhoneCodes:
		records, count, e := s.phones.List(ctx, q)
		if e != nil {
			return result, e
		}
		total = count
		for _, r := range records {
			result.Records = append(result.Records, phoneView(r, admin))
		}
	case Timezones:
		records, count, e := s.timezones.List(ctx, q)
		if e != nil {
			return result, e
		}
		total = count
		at := s.clock.Now()
		for _, r := range records {
			v, e := s.timezoneView(r, admin, at)
			if e != nil {
				return result, e
			}
			result.Records = append(result.Records, v)
		}
	case Flags:
		var records *globalflagger.FlagListResult
		if admin {
			records, err = s.flags.List(ctx, q)
		} else {
			records, err = s.flags.ListPublic(ctx, q)
		}
		if err != nil {
			return result, err
		}
		total = records.Total
		for _, r := range records.Flags {
			result.Records = append(result.Records, flagView(r, admin, false))
		}
	}
	if kind != Flags && len(result.Records) > 0 {
		flags, e := s.flagIndex(ctx, admin)
		if e != nil {
			return result, e
		}
		for i := range result.Records {
			if flag, ok := flags[result.Records[i].FlagID]; ok {
				result.Records[i].FlagURL = flagURL(flag.Code, flag.Revision, admin)
			}
		}
	}
	result.Cursor = nextCursor(kind, admin, q, total)
	return result, nil
}

// Build one bounded flag index per response instead of issuing a query per row.
func (s *Service) flagIndex(ctx context.Context, admin bool) (map[string]globalflagger.Record, error) {
	index := map[string]globalflagger.Record{}
	for page := 1; page <= 3; page++ {
		q := catalogue.ListQuery{Page: page, PageSize: 500, IncludeHidden: admin, IncludeDisabled: admin}
		var rows *globalflagger.FlagListResult
		var err error
		if admin {
			rows, err = s.flags.List(ctx, q)
		} else {
			rows, err = s.flags.ListPublic(ctx, q)
		}
		if err != nil {
			return nil, err
		}
		for _, row := range rows.Flags {
			index[row.Code] = row
		}
		if int64(page*500) >= rows.Total {
			return index, nil
		}
	}
	return nil, catalogue.ErrUnavailable
}

// Get returns one record by kind and code; non-admin callers receive only
// selectable records and never raw SVG. A referenced flag's URL is attached
// when that flag exists and remains selectable.
func (s *Service) Get(ctx context.Context, kind Kind, code string, admin bool) (result RecordView, err error) {
	defer func() { err = domainError(err) }()
	switch kind {
	case Currencies:
		r, e := s.currencies.Get(ctx, code)
		if e != nil {
			return result, e
		}
		result = currencyView(r, admin)
	case PhoneCodes:
		r, e := s.phones.Get(ctx, code)
		if e != nil {
			return result, e
		}
		result = phoneView(r, admin)
	case Timezones:
		r, e := s.timezones.Get(ctx, code)
		if e != nil {
			return result, e
		}
		result, err = s.timezoneView(r, admin, s.clock.Now())
		if err != nil {
			return result, err
		}
	case Flags:
		r, e := s.flags.Get(ctx, code)
		if e != nil {
			return result, e
		}
		result = flagView(*r, admin, true)
	default:
		return result, invalidHTTP()
	}
	if !admin && !isPublic(result) {
		return RecordView{}, catalogue.ErrNotFound
	}
	if result.FlagID != "" {
		flag, e := s.flags.Get(ctx, result.FlagID)
		if e != nil && !errors.Is(e, catalogue.ErrNotFound) {
			return RecordView{}, e
		}
		if e == nil && flag.DeletedAt == nil && (admin || catalogue.Selectable(flag.Enabled, flag.Hidden, flag.DeletedAt)) {
			result.FlagURL = flagURL(flag.Code, flag.Revision, admin)
		}
	}
	return result, nil
}

// validMutation rejects kind-specific fields applied to the wrong resource,
// attributing the failure to the offending field.
func validMutation(kind Kind, m Mutation) error {
	if !kind.valid() {
		return invalidHTTP()
	}
	if kind != Currencies && m.MinorUnit != nil {
		return invalidRecord("minor_unit")
	}
	if kind != PhoneCodes && (m.CallingCode != nil || m.DialPrefixes != nil) {
		return invalidRecord("calling_code")
	}
	if kind != Flags && m.SVG != nil {
		return invalidRecord("svg")
	}
	if kind == Flags && (m.FlagID != nil || m.RegionCodes != nil) {
		return invalidRecord("flag_id")
	}
	return nil
}

// mergeEntry applies non-nil mutation fields to a copy, and when FlagID changes
// to a non-empty value verifies the flag exists and is not deleted, mapping
// lookup misses to an invalid flag_id record error.
func (s *Service) mergeEntry(ctx context.Context, entry catalogue.Entry, m Mutation) (catalogue.Entry, error) {
	if m.Name != nil {
		entry.Name = *m.Name
	}
	if m.Enabled != nil {
		entry.Enabled = *m.Enabled
	}
	if m.Hidden != nil {
		entry.Hidden = *m.Hidden
	}
	if m.RegionCodes != nil {
		entry.RegionCodes = append([]string(nil), (*m.RegionCodes)...)
	}
	if m.FlagID != nil && *m.FlagID != entry.FlagID {
		if *m.FlagID != "" {
			flag, err := s.flags.Get(ctx, *m.FlagID)
			if errors.Is(err, catalogue.ErrNotFound) || errors.Is(err, catalogue.ErrInvalidPayload) {
				return entry, invalidRecord("flag_id")
			}
			if err != nil {
				return entry, err
			}
			if flag.DeletedAt != nil {
				return entry, invalidRecord("flag_id")
			}
		}
		entry.FlagID = *m.FlagID
	}
	return entry, nil
}

// Create persists a new record of the given kind with the actor as creator.
// Currencies and phone codes require known ISO definitions for the code (minor
// unit and calling data default from them); flags require an SVG payload.
// Validation failures return 422 field errors and child errors pass through
// domainError.
func (s *Service) Create(ctx context.Context, kind Kind, code string, m Mutation, actor string) (result RecordView, err error) {
	defer func() { err = domainError(err) }()
	if err = validMutation(kind, m); err != nil {
		return result, err
	}
	entry, err := s.mergeEntry(ctx, catalogue.Entry{Code: code}, m)
	if err != nil {
		return result, err
	}
	switch kind {
	case Currencies:
		precision, ok := currencycoder.MinorUnit(code)
		if !ok {
			return result, invalidRecord("code")
		}
		if m.MinorUnit != nil {
			precision = *m.MinorUnit
		}
		r, e := s.currencies.Create(ctx, currencycoder.CreateRequest{Record: currencycoder.Currency{Entry: entry, MinorUnit: precision}, ActorID: actor})
		if e != nil {
			return result, e
		}
		return currencyView(r, true), nil
	case PhoneCodes:
		r, ok := telenumcoder.Definition(code)
		if !ok {
			return result, invalidRecord("code")
		}
		r.Entry = entry
		if m.CallingCode != nil {
			r.CallingCode = *m.CallingCode
		}
		if m.DialPrefixes != nil {
			r.DialPrefixes = *m.DialPrefixes
		}
		r, err = s.phones.Create(ctx, telenumcoder.CreateRequest{Record: r, ActorID: actor})
		if err != nil {
			return result, err
		}
		return phoneView(r, true), nil
	case Timezones:
		r, e := s.timezones.Create(ctx, timezonecoder.CreateRequest{Record: timezonecoder.Timezone{Entry: entry}, ActorID: actor})
		if e != nil {
			return result, e
		}
		return s.timezoneView(r, true, s.clock.Now())
	case Flags:
		if m.SVG == nil {
			return result, invalidRecord("svg")
		}
		r, e := s.flags.Create(ctx, &globalflagger.CreateFlagRequest{Code: code, Name: entry.Name, Enabled: m.Enabled, Hidden: entry.Hidden, SVG: *m.SVG, ActorID: actor})
		if e != nil {
			return result, e
		}
		return flagView(*r, true, true), nil
	}
	return result, invalidHTTP()
}

// expectedInt narrows a revision to a positive int within int32 range,
// otherwise returning an invalid request.
func expectedInt(expected int64) (int, error) {
	if expected < 1 || expected > 2147483646 {
		return 0, invalidHTTP()
	}
	return int(expected), nil
}

// Update applies a compare-and-swap partial update for the kind: it reads the
// current record, merges mutation fields (flag changes are reference-checked)
// and submits with the expected revision and actor. Flags accept an optional
// replacement SVG but reject empty SVG values.
func (s *Service) Update(ctx context.Context, kind Kind, code string, m Mutation, expected int64, actor string) (result RecordView, err error) {
	defer func() { err = domainError(err) }()
	revision, err := expectedInt(expected)
	if err != nil {
		return result, err
	}
	if err = validMutation(kind, m); err != nil {
		return result, err
	}
	switch kind {
	case Currencies:
		r, e := s.currencies.Get(ctx, code)
		if e != nil {
			return result, e
		}
		r.Entry, err = s.mergeEntry(ctx, r.Entry, m)
		if err != nil {
			return result, err
		}
		if m.MinorUnit != nil {
			r.MinorUnit = *m.MinorUnit
		}
		r, err = s.currencies.Update(ctx, currencycoder.UpdateRequest{Record: r, ExpectedRevision: revision, ActorID: actor})
		if err != nil {
			return result, err
		}
		return currencyView(r, true), nil
	case PhoneCodes:
		r, e := s.phones.Get(ctx, code)
		if e != nil {
			return result, e
		}
		r.Entry, err = s.mergeEntry(ctx, r.Entry, m)
		if err != nil {
			return result, err
		}
		if m.CallingCode != nil {
			r.CallingCode = *m.CallingCode
		}
		if m.DialPrefixes != nil {
			r.DialPrefixes = *m.DialPrefixes
		}
		r, err = s.phones.Update(ctx, telenumcoder.UpdateRequest{Record: r, ExpectedRevision: revision, ActorID: actor})
		if err != nil {
			return result, err
		}
		return phoneView(r, true), nil
	case Timezones:
		r, e := s.timezones.Get(ctx, code)
		if e != nil {
			return result, e
		}
		r.Entry, err = s.mergeEntry(ctx, r.Entry, m)
		if err != nil {
			return result, err
		}
		r, err = s.timezones.Update(ctx, timezonecoder.UpdateRequest{Record: r, ExpectedRevision: revision, ActorID: actor})
		if err != nil {
			return result, err
		}
		return s.timezoneView(r, true, s.clock.Now())
	case Flags:
		old, e := s.flags.Get(ctx, code)
		if e != nil {
			return result, e
		}
		request := &globalflagger.UpdateFlagRequest{Code: code, Name: old.Name, Enabled: old.Enabled, Hidden: old.Hidden, ExpectedRevision: revision, ActorID: actor}
		if m.Name != nil {
			request.Name = *m.Name
		}
		if m.Enabled != nil {
			request.Enabled = *m.Enabled
		}
		if m.Hidden != nil {
			request.Hidden = *m.Hidden
		}
		if m.SVG != nil {
			if *m.SVG == "" {
				return result, invalidRecord("svg")
			}
			request.SVG = *m.SVG
		}
		r, e := s.flags.Update(ctx, request)
		if e != nil {
			return result, e
		}
		return flagView(*r, true, true), nil
	}
	return result, invalidHTTP()
}

// Remove soft-deletes a record by kind and code, requiring the current
// revision; it forwards to changeState with restore disabled.
func (s *Service) Remove(ctx context.Context, kind Kind, code string, expected int64, actor string) (RecordView, error) {
	return s.changeState(ctx, kind, code, expected, actor, false)
}

// Restore un-deletes a soft-deleted record by kind and code, requiring the
// revision observed while deleted; it forwards to changeState with restore
// enabled.
func (s *Service) Restore(ctx context.Context, kind Kind, code string, expected int64, actor string) (RecordView, error) {
	return s.changeState(ctx, kind, code, expected, actor, true)
}

// changeState performs the delete or restore against the owning child service
// with the expected revision and actor, mapping domain failures through
// domainError and rejecting unknown kinds.
func (s *Service) changeState(ctx context.Context, kind Kind, code string, expected int64, actor string, restore bool) (result RecordView, err error) {
	defer func() { err = domainError(err) }()
	revision, err := expectedInt(expected)
	if err != nil {
		return result, err
	}
	request := catalogue.ChangeRequest{Code: code, ExpectedRevision: revision, ActorID: actor}
	switch kind {
	case Currencies:
		var r currencycoder.Currency
		if restore {
			r, err = s.currencies.Restore(ctx, request)
		} else {
			r, err = s.currencies.Delete(ctx, request)
		}
		if err != nil {
			return result, err
		}
		return currencyView(r, true), nil
	case PhoneCodes:
		var r telenumcoder.Country
		if restore {
			r, err = s.phones.Restore(ctx, request)
		} else {
			r, err = s.phones.Delete(ctx, request)
		}
		if err != nil {
			return result, err
		}
		return phoneView(r, true), nil
	case Timezones:
		var r timezonecoder.Timezone
		if restore {
			r, err = s.timezones.Restore(ctx, request)
		} else {
			r, err = s.timezones.Delete(ctx, request)
		}
		if err != nil {
			return result, err
		}
		return s.timezoneView(r, true, s.clock.Now())
	case Flags:
		var r *globalflagger.Record
		if restore {
			r, err = s.flags.Restore(ctx, &globalflagger.RestoreFlagRequest{Code: code, ExpectedRevision: revision, ActorID: actor})
		} else {
			r, err = s.flags.Delete(ctx, &globalflagger.DeleteFlagRequest{Code: code, ExpectedRevision: revision, ActorID: actor})
		}
		if err != nil {
			return result, err
		}
		return flagView(*r, true, true), nil
	}
	return result, invalidHTTP()
}

// NormalisePhone delegates parsing and catalogue selection to the owning phone service without checking SMS reachability.
func (s *Service) NormalisePhone(ctx context.Context, phone, region string) (telenumcoder.Number, error) {
	return s.phones.Normalise(ctx, phone, region)
}

// CheckPhone normalises and validates a phone number for a region; an unknown
// or non-selectable region maps to a 422 region_code field error and other
// failures pass through domainError.
func (s *Service) CheckPhone(ctx context.Context, phone, region string) (PhoneCheck, error) {
	result, err := s.phones.Check(ctx, phone, region)
	if err != nil {
		if errors.Is(err, catalogue.ErrNotFound) || errors.Is(err, catalogue.ErrNotSelectable) {
			return PhoneCheck{}, invalidRecord("region_code")
		}
		return PhoneCheck{}, domainError(err)
	}
	return PhoneCheck{NormalisedNumber: result.NormalisedNumber, RegionCode: result.RegionCode, State: string(result.State), Channel: result.Channel}, nil
}

// Selection policy is rechecked by the agreement manager at new-choice boundaries.
// Reachability is a separate, explicit operation and never runs in a transaction.
func (s *Service) ValidateCurrency(ctx context.Context, code string) error {
	_, err := s.currencies.RequireSelectable(ctx, code)
	return err
}

// ValidatePhone normalises a number with an empty region and returns any
// validation error.
func (s *Service) ValidatePhone(ctx context.Context, number string) error {
	_, err := s.phones.Normalise(ctx, number, "")
	return err
}

// ValidateTimezone requires the zone to exist and be selectable, returning any
// failure.
func (s *Service) ValidateTimezone(ctx context.Context, zone string) error {
	_, err := s.timezones.RequireSelectable(ctx, zone)
	return err
}
