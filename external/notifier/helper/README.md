# Notification credential helpers

Package `notifierhelper` supplies optional startup validation over explicit
strings/bytes. Import `github.com/ooaklee/ghatd/external/notifier/helper`.
Neither function reads environment variables/files, creates temporary files,
contacts providers, constructs senders or enables a channel.

## VAPID pairs

`ValidateVAPIDKeyPair(public, private)` accepts URL-base64 strings with or without
trailing padding. It parses a P-256 private key and compares its derived public
key with the supplied public bytes. Decode, key-shape and mismatch failures all
return `ErrVAPIDKeyPairInvalid` without credential material.

## Explicit FCM service-account profile

`ValidateFCMServiceAccountJSON(raw, expectedProjectID)` requires a nonblank
expected project, exact `project_id`, `service_account` type, a client email
ending in `.iam.gserviceaccount.com`, the token URI
`https://oauth2.googleapis.com/token`, and a PEM-encoded PKCS8 RSA private key.
Project matching does not trim or normalize either identity.

This is a deliberately strict **optional profile**, not universal Firebase SDK
credential acceptance. PKCS1 keys and application-default credentials can be
valid for other integrations but do not qualify for this profile. Passing these
checks does not prove the key/account is authentic, active or permitted by Google.

JSON/identity failures return `ErrFCMServiceAccountInvalid`. Missing/unparsable
PKCS8 keys return `ErrFCMSigningKeyInvalid`; other PKCS8 key types return
`ErrFCMRSASigningKeyRequired`. Errors are fixed sentinels without JSON, paths,
project IDs or key material. These startup-validation utilities are outside the
HTTP error manifest; hosts map them to their configuration-field diagnostics.

## Host and sender ownership

Hosts retain secret loading, base64-versus-file precedence, file lifetime and
explicit channel switches. Pass only resolved bytes to the FCM check; handle
loading errors separately without exposing filesystem/secret values. Disabled
channels can ignore their credentials according to host policy.

The existing [notifier sender factory](../README.md#4-use-the-sender-factory) and SDK
remain unchanged: these checks are opt-in, are not invoked by
`NewStandardSenders`, and do not change implicit Web Push enablement, FCM
project/default-credential support or delivery behavior.
