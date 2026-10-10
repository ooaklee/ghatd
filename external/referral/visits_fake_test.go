package referral

import (
	"context"
	"fmt"
	"time"
)

// Mutable transaction fake for visit tests; complete copies commit only on
// callback success, exercising receipt/observation rollback as one boundary.
func (f *fakeRepo) WithVisitTransaction(ctx context.Context, link string, fn func(Repository) error) error {
	if ctx == nil || link == "" || fn == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	tx := newFakeRepo()
	tx.fail = f.fail
	for k, v := range f.links {
		tx.links[k] = v
	}
	for k, v := range f.history {
		tx.history[k] = append([]Referral(nil), v...)
	}
	for k, v := range f.bindings {
		tx.bindings[k] = v
	}
	for k, v := range f.visits {
		tx.visits[k] = v
	}
	for k, v := range f.days {
		tx.days[k] = v
	}
	tx.clicks = append([]Click(nil), f.clicks...)
	if err := fn(tx); err != nil {
		return err
	}
	f.links, f.history, f.bindings, f.visits, f.clicks, f.days = tx.links, tx.history, tx.bindings, tx.visits, tx.clicks, tx.days
	return nil
}

func (f *fakeRepo) GetClick(_ context.Context, link, id string) (Click, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return Click{}, f.fail
	}
	for _, c := range f.clicks {
		if c.LinkID == link && c.ID == id {
			return c, nil
		}
	}
	return Click{}, ErrNotFound
}
func (f *fakeRepo) GetVisitReceipt(_ context.Context, link, digest string) (VisitReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return VisitReceipt{}, f.fail
	}
	v, ok := f.visits[link+":"+digest]
	if !ok {
		return VisitReceipt{}, ErrNotFound
	}
	return v, nil
}
func (f *fakeRepo) InsertVisitReceipt(_ context.Context, v VisitReceipt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	k := v.LinkID + ":" + v.Digest
	if _, ok := f.visits[k]; ok {
		return ErrAlreadyExists
	}
	f.visits[k] = v
	return nil
}

func fakeDayKey(link string, day time.Time) string {
	return fmt.Sprintf("%s:%s", link, day.Format("2006-01-02"))
}
func (f *fakeRepo) GetVisitDay(_ context.Context, link string, day time.Time) (VisitDay, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return VisitDay{}, f.fail
	}
	d, ok := f.days[fakeDayKey(link, day)]
	if !ok {
		return VisitDay{}, ErrNotFound
	}
	return d, nil
}
func (f *fakeRepo) PutVisitDay(_ context.Context, d VisitDay, expected int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	k := fakeDayKey(d.LinkID, d.Day)
	if f.days[k].Revision != expected {
		return ErrStaleWrite
	}
	f.days[k] = d
	return nil
}
