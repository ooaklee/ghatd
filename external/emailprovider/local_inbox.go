package emailprovider

import (
	"sort"
	"sync"
	"time"
)

const defaultLocalEmailStoreLimit = 50

// LocalEmail is a captured email intended for local development previews.
type LocalEmail struct {
	MessageID string    `json:"messageId"`
	To        string    `json:"to"`
	From      string    `json:"from"`
	ReplyTo   string    `json:"replyTo,omitempty"`
	Subject   string    `json:"subject"`
	HTMLBody  string    `json:"htmlBody"`
	TextBody  string    `json:"textBody,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// LocalEmailStore keeps a bounded in-memory list of captured local emails.
type LocalEmailStore struct {
	mu     sync.RWMutex
	limit  int
	emails []LocalEmail
}

// NewLocalEmailStore creates an in-memory local email store.
func NewLocalEmailStore(limit int) *LocalEmailStore {
	if limit <= 0 {
		limit = defaultLocalEmailStoreLimit
	}

	return &LocalEmailStore{
		limit:  limit,
		emails: make([]LocalEmail, 0, limit),
	}
}

// Add stores an email and trims older entries when the store exceeds its limit.
func (s *LocalEmailStore) Add(email LocalEmail) LocalEmail {
	if s == nil {
		return email
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.emails = append(s.emails, email)
	if len(s.emails) > s.limit {
		s.emails = append([]LocalEmail(nil), s.emails[len(s.emails)-s.limit:]...)
	}

	return email
}

// List returns captured emails newest-first.
func (s *LocalEmailStore) List() []LocalEmail {
	if s == nil {
		return []LocalEmail{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	type indexedEmail struct {
		email LocalEmail
		index int
	}

	emails := make([]indexedEmail, 0, len(s.emails))
	for index, email := range s.emails {
		emails = append(emails, indexedEmail{
			email: email,
			index: index,
		})
	}

	sort.SliceStable(emails, func(i, j int) bool {
		if emails[i].email.CreatedAt.Equal(emails[j].email.CreatedAt) {
			return emails[i].index > emails[j].index
		}
		return emails[i].email.CreatedAt.After(emails[j].email.CreatedAt)
	})

	result := make([]LocalEmail, 0, len(emails))
	for _, item := range emails {
		result = append(result, item.email)
	}

	return result
}

// Get returns a captured email by message ID.
func (s *LocalEmailStore) Get(messageID string) (LocalEmail, bool) {
	if s == nil {
		return LocalEmail{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, email := range s.emails {
		if email.MessageID == messageID {
			return email, true
		}
	}

	return LocalEmail{}, false
}

// Delete removes a captured email by message ID.
func (s *LocalEmailStore) Delete(messageID string) bool {
	if s == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, email := range s.emails {
		if email.MessageID == messageID {
			s.emails = append(s.emails[:i], s.emails[i+1:]...)
			return true
		}
	}

	return false
}

// Clear removes all captured emails.
func (s *LocalEmailStore) Clear() {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.emails = s.emails[:0]
}

// Count returns the number of captured emails.
func (s *LocalEmailStore) Count() int {
	if s == nil {
		return 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.emails)
}
