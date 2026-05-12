// Package bundles provides reusable error manifest bundles for common GHATD
// composition paths.
//
// The package intentionally lives below errormanifest instead of inside it.
// Several domain packages already import errormanifest for Composer support,
// so keeping bundle helpers in this subpackage avoids import cycles while
// still giving starter packages and applications a named place to reuse the
// standard cross-package error maps.
package bundles
