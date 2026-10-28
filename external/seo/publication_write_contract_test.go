package seo

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type publicationWriteLegacyRepository struct {
	*fakeSitemapRepository
	creates, updates, upserts int
	publicationAfterRead      *string
}

func (r *publicationWriteLegacyRepository) CreateSitemapItem(ctx context.Context, item *SitemapItem) (*SitemapItem, error) {
	r.creates++
	return r.fakeSitemapRepository.CreateSitemapItem(ctx, item)
}

func (r *publicationWriteLegacyRepository) UpdateSitemapItemByURI(ctx context.Context, item *SitemapItem) (*SitemapItem, error) {
	r.updates++
	return r.fakeSitemapRepository.UpdateSitemapItemByURI(ctx, item)
}

func (r *publicationWriteLegacyRepository) UpsertSitemapItemByURI(ctx context.Context, item *SitemapItem) (*SitemapItem, bool, error) {
	r.upserts++
	return r.fakeSitemapRepository.UpsertSitemapItemByURI(ctx, item)
}

func (r *publicationWriteLegacyRepository) GetSitemapItemByURI(ctx context.Context, uri string) (*SitemapItem, error) {
	stale, err := r.fakeSitemapRepository.GetSitemapItemByURI(ctx, uri)
	if err == nil && r.publicationAfterRead != nil {
		newer := *stale
		newer.PublishedAt = *r.publicationAfterRead
		r.items[newer.URI] = newer
		r.publicationAfterRead = nil
	}
	return stale, err
}

type publicationWriteCall struct {
	item        SitemapItem
	publication *string
}

type publicationWriteRepository struct {
	*publicationWriteLegacyRepository
	publicationUpdates []publicationWriteCall
	publicationUpserts []publicationWriteCall
}

func (r *publicationWriteRepository) UpdateSitemapItemWithPublication(_ context.Context, item *SitemapItem, publication *string) (*SitemapItem, error) {
	r.publicationUpdates = append(r.publicationUpdates, publicationWriteCall{item: *item, publication: copyPublicationWriteString(publication)})
	stored, _ := r.storePublication(item, publication)
	return stored, nil
}

func (r *publicationWriteRepository) UpsertSitemapItemWithPublication(_ context.Context, item *SitemapItem, publication *string) (*SitemapItem, bool, error) {
	r.publicationUpserts = append(r.publicationUpserts, publicationWriteCall{item: *item, publication: copyPublicationWriteString(publication)})
	stored, created := r.storePublication(item, publication)
	return stored, created, nil
}

func (r *publicationWriteRepository) storePublication(item *SitemapItem, publication *string) (*SitemapItem, bool) {
	previous, exists := r.items[item.URI]
	stored := *item
	if publication == nil {
		stored.PublishedAt = previous.PublishedAt
	} else {
		stored.PublishedAt = *publication
	}
	r.items[item.URI] = stored
	return &stored, !exists
}

func newPublicationWriteRepository(items ...SitemapItem) *publicationWriteRepository {
	return &publicationWriteRepository{publicationWriteLegacyRepository: &publicationWriteLegacyRepository{
		fakeSitemapRepository: newFakeSitemapRepository(items),
	}}
}

func publicationWriteString(value string) *string { return &value }

func copyPublicationWriteString(value *string) *string {
	if value == nil {
		return nil
	}
	return publicationWriteString(*value)
}

func publicationWriteFixture(uri, publication string) SitemapItem {
	return SitemapItem{URI: uri, LastMod: "2026-10-01T12:00:00Z", Priority: 0.5, ChangeFrequency: ChangeFrequencyWeekly, PublishedAt: publication}
}

func TestPublicationWriteCreationPropagation(t *testing.T) {
	tests := []struct {
		name        string
		publication *string
		existing    bool
		legacy      bool
		want        string
	}{
		{name: "omitted old client with capable repository"},
		{name: "omitted old client with legacy repository", legacy: true},
		{name: "explicit immediate", publication: publicationWriteString("")},
		{name: "UTC normalisation retains nanoseconds", publication: publicationWriteString("2026-11-01T10:00:00.123456789+01:00"), want: "2026-11-01T09:00:00.123456789Z"},
		{name: "existing schedule is skipped", publication: publicationWriteString("2026-12-01T09:00:00Z"), existing: true, want: "2026-11-01T09:00:00Z"},
		{name: "legacy existing schedule can be read without mutation", existing: true, legacy: true, want: "2026-11-01T09:00:00Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, batch := range []bool{false, true} {
				name := "single create"
				if batch {
					name = "non-overriding batch"
				}
				t.Run(name, func(t *testing.T) {
					repo := newPublicationWriteRepository()
					if tc.existing {
						repo.items["/entry"] = publicationWriteFixture("/entry", "2026-11-01T09:00:00Z")
					}
					var adapter SitemapRepository = repo
					if tc.legacy {
						adapter = repo.publicationWriteLegacyRepository
					}
					service := NewService(&NewServiceRequest{SitemapRepository: adapter})
					req := CreateSitemapItemRequest{URI: "/entry", LastMod: "2026-10-01T12:00:00Z", PublishedAt: tc.publication}
					var got *SitemapItem
					if batch {
						response, err := service.MassSitemapItemCreationByBatch(context.Background(), &MassSitemapItemCreationByBatchRequest{Items: []CreateSitemapItemRequest{req}})
						if err != nil || response == nil || len(response.SitemapItems) != 1 {
							t.Fatalf("batch response = %+v, error = %v", response, err)
						}
						if tc.existing && (response.Skipped != 1 || response.Created != 0) || !tc.existing && (response.Created != 1 || response.Skipped != 0) {
							t.Fatalf("unexpected batch counts: %+v", response)
						}
						got = &response.SitemapItems[0]
					} else {
						response, err := service.CreateSitemapItemIfDoesNotAlreadyExist(context.Background(), &req)
						if err != nil || response == nil || response.Created == tc.existing {
							t.Fatalf("create response = %+v, error = %v", response, err)
						}
						got = response.SitemapItem
					}
					if got.PublishedAt != tc.want || repo.items["/entry"].PublishedAt != tc.want {
						t.Fatalf("publication response = %q, stored = %q; want %q", got.PublishedAt, repo.items["/entry"].PublishedAt, tc.want)
					}
					if got.LastMod != req.LastMod {
						t.Fatalf("publication changed last_mod: got %q, want %q", got.LastMod, req.LastMod)
					}
					wantCreates := 1
					if tc.existing {
						wantCreates = 0
					}
					if repo.creates != wantCreates || repo.updates != 0 || repo.upserts != 0 || len(repo.publicationUpserts) != 0 || len(repo.publicationUpdates) != 0 {
						t.Fatalf("unexpected writes: creates=%d updates=%d upserts=%d atomic updates=%d atomic upserts=%d", repo.creates, repo.updates, repo.upserts, len(repo.publicationUpdates), len(repo.publicationUpserts))
					}
				})
			}
		})
	}
}

func TestPublicationWriteOverridePatch(t *testing.T) {
	tests := []struct {
		name        string
		publication *string
		existing    bool
		legacy      bool
		want        string
	}{
		{name: "omitted preserves schedule", existing: true, want: "2026-11-01T09:00:00Z"},
		{name: "explicit empty clears schedule", existing: true, publication: publicationWriteString("")},
		{name: "explicit value reschedules", existing: true, publication: publicationWriteString("2026-12-01T10:00:00+01:00"), want: "2026-12-01T09:00:00Z"},
		{name: "omitted new upsert is immediate"},
		{name: "explicit schedule on new upsert", publication: publicationWriteString("2026-12-01T10:00:00+01:00"), want: "2026-12-01T09:00:00Z"},
		{name: "old adapter keeps no-date override compatibility", existing: true, legacy: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newPublicationWriteRepository()
			if tc.existing {
				repo.items["/entry"] = publicationWriteFixture("/entry", "2026-11-01T09:00:00Z")
				if tc.legacy {
					repo.items["/entry"] = publicationWriteFixture("/entry", "")
				}
			}
			var adapter SitemapRepository = repo
			if tc.legacy {
				adapter = repo.publicationWriteLegacyRepository
			}
			service := NewService(&NewServiceRequest{SitemapRepository: adapter})
			response, err := service.MassSitemapItemCreationByBatch(context.Background(), &MassSitemapItemCreationByBatchRequest{
				OverrideIfExists: true,
				Items:            []CreateSitemapItemRequest{{URI: "/entry", LastMod: "2026-10-02T12:00:00Z", PublishedAt: tc.publication}},
			})
			if err != nil || response == nil || len(response.SitemapItems) != 1 {
				t.Fatalf("batch response = %+v, error = %v", response, err)
			}
			if response.SitemapItems[0].PublishedAt != tc.want || repo.items["/entry"].PublishedAt != tc.want {
				t.Fatalf("publication response = %q, stored = %q; want %q", response.SitemapItems[0].PublishedAt, repo.items["/entry"].PublishedAt, tc.want)
			}
			if tc.existing && (response.Updated != 1 || response.Created != 0) || !tc.existing && (response.Created != 1 || response.Updated != 0) {
				t.Fatalf("unexpected batch counts: %+v", response)
			}
			if tc.legacy {
				if repo.upserts != 1 || len(repo.publicationUpserts) != 0 {
					t.Fatalf("legacy upsert calls: atomic=%d, legacy=%d", len(repo.publicationUpserts), repo.upserts)
				}
			} else {
				if len(repo.publicationUpserts) != 1 || repo.upserts != 0 {
					t.Fatalf("upsert calls: atomic=%d, legacy=%d", len(repo.publicationUpserts), repo.upserts)
				}
				patch := repo.publicationUpserts[0].publication
				if (patch == nil) != (tc.publication == nil) || patch != nil && *patch != tc.want {
					t.Fatalf("publication patch = %v; request = %v, normalized target = %q", patch, tc.publication, tc.want)
				}
			}
			if repo.items["/entry"].LastMod != "2026-10-02T12:00:00Z" {
				t.Fatalf("metadata was not updated with publication: %+v", repo.items["/entry"])
			}
		})
	}
}

func TestPublicationWriteEditPatch(t *testing.T) {
	tests := []struct {
		name        string
		publication *string
		want        string
	}{
		{name: "omitted preserves stored schedule", want: "2026-11-01T09:00:00Z"},
		{name: "empty clears", publication: publicationWriteString("")},
		{name: "whitespace clears", publication: publicationWriteString("  ")},
		{name: "explicit value reschedules in UTC", publication: publicationWriteString("2026-12-01T10:00:00.123456789+01:00"), want: "2026-12-01T09:00:00.123456789Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newPublicationWriteRepository(publicationWriteFixture("/entry", "2026-11-01T09:00:00Z"))
			service := NewService(&NewServiceRequest{SitemapRepository: repo})
			response, err := service.UpdateSitemapItemByUri(context.Background(), &UpdateSitemapItemRequest{URI: "/entry", PublishedAt: tc.publication})
			if err != nil || response == nil || !response.Updated {
				t.Fatalf("edit response=%+v error=%v", response, err)
			}
			if response.SitemapItem.PublishedAt != tc.want || repo.items["/entry"].PublishedAt != tc.want {
				t.Fatalf("publication response=%q stored=%q want=%q", response.SitemapItem.PublishedAt, repo.items["/entry"].PublishedAt, tc.want)
			}
			if len(repo.publicationUpdates) != 1 || repo.updates != 0 {
				t.Fatalf("update calls: atomic=%d legacy=%d", len(repo.publicationUpdates), repo.updates)
			}
			patch := repo.publicationUpdates[0].publication
			if (patch == nil) != (tc.publication == nil) || patch != nil && *patch != tc.want {
				t.Fatalf("incorrect publication patch=%v request=%v normalized=%q", patch, tc.publication, tc.want)
			}
			if response.SitemapItem.LastMod != "2026-10-01T12:00:00Z" {
				t.Fatalf("publication edit changed last_mod: %+v", response.SitemapItem)
			}
		})
	}
}

func TestPublicationWriteOmittedEditPreservesConcurrentState(t *testing.T) {
	tests := []struct{ name, initial, concurrent string }{
		{name: "schedule changed", initial: "2026-11-01T09:00:00Z", concurrent: "2026-12-01T09:00:00Z"},
		{name: "schedule added", concurrent: "2026-12-01T09:00:00Z"},
		{name: "schedule cleared", initial: "2026-11-01T09:00:00Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newPublicationWriteRepository(publicationWriteFixture("/entry", tc.initial))
			repo.publicationAfterRead = publicationWriteString(tc.concurrent)
			service := NewService(&NewServiceRequest{SitemapRepository: repo})
			var req UpdateSitemapItemRequest
			if err := json.Unmarshal([]byte(`{"uri":"/entry","last_mod":"2026-10-03T12:00:00Z"}`), &req); err != nil {
				t.Fatal(err)
			}
			response, err := service.UpdateSitemapItemByUri(context.Background(), &req)
			if err != nil || response == nil || !response.Updated {
				t.Fatalf("update response = %+v, error = %v", response, err)
			}
			if response.SitemapItem.PublishedAt != tc.concurrent || repo.items["/entry"].PublishedAt != tc.concurrent {
				t.Fatalf("concurrent publication lost: response=%q stored=%q want=%q", response.SitemapItem.PublishedAt, repo.items["/entry"].PublishedAt, tc.concurrent)
			}
			if len(repo.publicationUpdates) != 1 || repo.publicationUpdates[0].publication != nil || repo.updates != 0 {
				t.Fatalf("omitted update must use one nil atomic patch: calls=%+v legacy calls=%d", repo.publicationUpdates, repo.updates)
			}
			if repo.publicationUpdates[0].item.PublishedAt != tc.initial || response.SitemapItem.LastMod != req.LastMod {
				t.Fatalf("fixture did not exercise a stale read and metadata update: call=%+v response=%+v", repo.publicationUpdates[0], response)
			}
		})
	}
}

func TestPublicationWriteBatchPreflight(t *testing.T) {
	invalidPriority := 2.0
	tests := []struct {
		name     string
		second   CreateSitemapItemRequest
		legacy   bool
		override bool
		stored   string
		wantErr  error
	}{
		{name: "invalid publication nonoverride", second: CreateSitemapItemRequest{URI: "/second", PublishedAt: publicationWriteString("tomorrow")}, wantErr: ErrSitemapItemInvalidPublishedAt},
		{name: "invalid publication override", second: CreateSitemapItemRequest{URI: "/second", PublishedAt: publicationWriteString("2026-02-30T09:00:00Z")}, override: true, wantErr: ErrSitemapItemInvalidPublishedAt},
		{name: "invalid URI", second: CreateSitemapItemRequest{URI: "//invalid"}, wantErr: ErrSitemapItemInvalidURI},
		{name: "invalid priority", second: CreateSitemapItemRequest{URI: "/second", Priority: &invalidPriority}, override: true, wantErr: ErrSitemapItemInvalidPriority},
		{name: "invalid last modified", second: CreateSitemapItemRequest{URI: "/second", LastMod: "yesterday"}, wantErr: ErrSitemapItemInvalidLastMod},
		{name: "invalid frequency", second: CreateSitemapItemRequest{URI: "/second", ChangeFrequency: "sometimes"}, override: true, wantErr: ErrSitemapItemInvalidChangeFrequency},
		{name: "legacy explicit schedule", second: CreateSitemapItemRequest{URI: "/second", PublishedAt: publicationWriteString("2026-12-01T09:00:00Z")}, legacy: true, wantErr: ErrSitemapItemPublicationScheduleUnsupported},
		{name: "legacy explicit clear", second: CreateSitemapItemRequest{URI: "/second", PublishedAt: publicationWriteString("")}, legacy: true, override: true, wantErr: ErrSitemapItemPublicationScheduleUnsupported},
		{name: "legacy known scheduled override", second: CreateSitemapItemRequest{URI: "/second"}, legacy: true, override: true, stored: "2026-11-01T09:00:00Z", wantErr: ErrSitemapItemPublicationScheduleUnsupported},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newPublicationWriteRepository(publicationWriteFixture("/second", tc.stored))
			var adapter SitemapRepository = repo
			if tc.legacy {
				adapter = repo.publicationWriteLegacyRepository
			}
			service := NewService(&NewServiceRequest{SitemapRepository: adapter})
			response, err := service.MassSitemapItemCreationByBatch(context.Background(), &MassSitemapItemCreationByBatchRequest{
				Items: []CreateSitemapItemRequest{{URI: "/valid-first"}, tc.second}, OverrideIfExists: tc.override,
			})
			if !errors.Is(err, tc.wantErr) || response != nil {
				t.Fatalf("batch response=%+v error=%v; want nil response and %v", response, err, tc.wantErr)
			}
			if repo.creates+repo.updates+repo.upserts+len(repo.publicationUpdates)+len(repo.publicationUpserts) != 0 {
				t.Fatalf("preflight failure performed writes: creates=%d updates=%d upserts=%d atomic updates=%d atomic upserts=%d", repo.creates, repo.updates, repo.upserts, len(repo.publicationUpdates), len(repo.publicationUpserts))
			}
			if len(repo.items) != 1 || repo.items["/second"].PublishedAt != tc.stored {
				t.Fatalf("preflight failure changed stored records: %+v", repo.items)
			}
		})
	}
}

func TestPublicationWriteLegacyMutationCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		publication *string
		stored      string
		wantErr     error
	}{
		{name: "omitted legacy metadata edit"},
		{name: "explicit schedule rejected", publication: publicationWriteString("2026-12-01T09:00:00Z"), wantErr: ErrSitemapItemPublicationScheduleUnsupported},
		{name: "explicit clear rejected", publication: publicationWriteString(""), wantErr: ErrSitemapItemPublicationScheduleUnsupported},
		{name: "known schedule edit rejected", stored: "2026-11-01T09:00:00Z", wantErr: ErrSitemapItemPublicationScheduleUnsupported},
		{name: "invalid publication rejected", publication: publicationWriteString("0001-01-01T00:00:00Z"), wantErr: ErrSitemapItemInvalidPublishedAt},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newPublicationWriteRepository(publicationWriteFixture("/entry", tc.stored)).publicationWriteLegacyRepository
			service := NewService(&NewServiceRequest{SitemapRepository: repo})
			response, err := service.UpdateSitemapItemByUri(context.Background(), &UpdateSitemapItemRequest{URI: "/entry", LastMod: "2026-10-03T12:00:00Z", PublishedAt: tc.publication})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("update error=%v; want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if response != nil || repo.updates != 0 || repo.items["/entry"].LastMod != "2026-10-01T12:00:00Z" {
					t.Fatalf("rejected mutation changed storage: response=%+v calls=%d stored=%+v", response, repo.updates, repo.items["/entry"])
				}
				return
			}
			if response == nil || !response.Updated || repo.updates != 1 || response.SitemapItem.LastMod != "2026-10-03T12:00:00Z" || response.SitemapItem.PublishedAt != "" {
				t.Fatalf("legacy metadata compatibility failed: response=%+v calls=%d", response, repo.updates)
			}
		})
	}
}
