// Package notifierhelper offers optional, passive startup checks for VAPID key
// pairs and an explicit PKCS8-RSA FCM service-account profile. Hosts supply
// resolved secrets and retain source selection, enablement and lifetime. These
// checks do not authenticate credentials or change existing sender behavior.
// See README.md for profile constraints and fixed startup-error semantics.
package notifierhelper
