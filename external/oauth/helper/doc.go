// Package oauthhelper supplies optional signing-key loading from explicit host
// inputs. It never reads environment variables or chooses provider configuration.
// Provider constructors retain PEM/curve validation; hosts retain key lifetime,
// secret storage and completeness policy. Canonical contracts are in README.md.
package oauthhelper
