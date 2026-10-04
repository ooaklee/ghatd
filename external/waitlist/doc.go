// Package waitlist provides optional prerelease signup, consent, private export
// and a single bounded announcement campaign. Hosts supply managed dependencies
// and guards; GHATD owns stable signup identities. Optional transactional enrollment
// sequences, consent configuration, CSV projections and trusted content hooks keep
// host policy separate from shared delivery. It is not a bulk mail scheduler.
package waitlist
