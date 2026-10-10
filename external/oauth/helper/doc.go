// Package oauthhelper supplies optional secure-provider construction and signing
// key loading from explicit host inputs. All-blank providers remain disabled;
// partial inputs fail with redacted errors. Native constructors validate callback
// and key policy. Hosts retain secret storage, registration and runtime lifetime.
// Canonical contracts are in README.md.
package oauthhelper
