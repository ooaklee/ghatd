package referral

import "context"

// Snapshot fake holds the owning mutex for all scopes rather than stitching
// together service list calls, preserving the production port's read boundary.
func (f *fakeRepo) ReadAnalyticsSnapshot(ctx context.Context, program, partner string, q AnalyticsQuery) (AnalyticsSnapshot, error) {
	if ctx == nil {
		return AnalyticsSnapshot{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return AnalyticsSnapshot{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return AnalyticsSnapshot{}, f.fail
	}
	out := AnalyticsSnapshot{}
	links := map[string]bool{}
	for _, l := range f.links {
		if l.ProgramID == program && l.PartnerID == partner {
			out.Links = append(out.Links, l)
			links[l.ID] = true
		}
	}
	for _, d := range f.days {
		if links[d.LinkID] {
			out.Days = append(out.Days, d)
		}
	}
	for _, c := range f.clicks {
		if links[c.LinkID] {
			for _, day := range q.PartialDays() {
				if utcDay(c.OccurredAt).Equal(day) {
					out.Clicks = append(out.Clicks, c)
				}
			}
		}
	}
	for _, history := range f.history {
		first := ""
		for _, r := range history {
			if r.ProgramID == program && r.PartnerID == partner && first == "" {
				first = r.ID
			}
		}
		if first == "" {
			continue
		}
		head := history[len(history)-1]
		out.Relationships = append(out.Relationships, RelationshipSnapshot{ID: RelationshipReferenceID(program, partner, head.ReferredCustomer), ProgramID: program, PartnerID: partner, ReferredCustomer: head.ReferredCustomer, FirstReferralID: first, Head: head, History: append([]Referral(nil), history...)})
	}
	return out, nil
}
