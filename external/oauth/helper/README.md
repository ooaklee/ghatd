# OAuth configuration helpers

`oauthhelper.LoadAppleSigningKey(encoded, path)` resolves signing-key bytes from
explicit trusted host inputs. It does not select a provider, read environment
variables, contact a secret service, write files or validate the PEM key.

A non-empty standard-base64 value takes precedence over the supplied file path.
Decoding is strict and in memory. Invalid or empty decoded bytes return the fixed
error `apple OAuth signing key base64 is invalid`, without file fallback.
When `encoded` is empty, the helper reads only `path`; read failures return
`apple OAuth signing key cannot be read`. Errors omit both inputs and filesystem
causes. An empty file is returned unchanged for provider validation.

Pass the result to [oauth.NewAppleProvider](../README.md) as `PrivateKeyPEM`;
that owner validates PKCS#8 PEM and the P-256 curve. Hosts keep all-empty providers
disabled, reject partial configuration, manage key lifetime/rotation and supply
file permissions or secret storage. The helper performs explicit file I/O only
in file mode, so invoke it during controlled startup rather than request handling.

```go
import oauthhelper "github.com/ooaklee/ghatd/external/oauth/helper"

key, err := oauthhelper.LoadAppleSigningKey(encodedKey, signingKeyPath)
// Check err before passing key to oauth.NewAppleProviderRequest.PrivateKeyPEM.
```

The [composition example](../../../examples/oauth/README.md) and
[adoption guide](../../../docs/how-to/add-google-apple-sign-in.md) explain complete
provider configuration. Environment variable names remain host conventions.

## Upgrade from the example helper

The exported `examples/oauth.LoadAppleSigningKey` moved to this optional package
in the unreleased 0.5.0 change. Update its import to `external/oauth/helper` and
use the `oauthhelper` package name. The signature, precedence, returned bytes and
fixed errors are unchanged. The example package no longer exports this loader;
there is no compatibility forwarder or stored-data migration.

```sh
go test -race ./external/oauth/helper
```
