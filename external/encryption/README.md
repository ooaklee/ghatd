# Authenticated payload encryption

`NewPayloadCipher(key)` creates an immutable AES-256-GCM cipher from an explicit
32-byte key. `Seal(plaintext, identity)` and `Open(ciphertext, identity)` require
nonempty additional authenticated data (AAD). Inputs are neither retained nor
modified; failures return no plaintext. Use a stable key from your secret manager.

The byte format is **12-byte nonce | ciphertext | 16-byte authentication tag**.
Every seal uses a cryptographically random nonce. This deliberately preserves
the standard nonce-prefixed format; it does not prepend a version or key ID.
Do not encrypt more than 2^32 messages with a single key.

```go
payloadCipher, err := encryption.NewPayloadCipher(key)
if err != nil {
    return err
}
sealed, err := payloadCipher.Seal(payload, identity)
```

The host owns schemas, encryption boundaries, identity construction, key storage,
rotation and recovery. Bind AAD to an unambiguous domain/resource/revision tuple;
do not concatenate unconstrained strings with a delimiter. When migrating an
existing application, preserve its exact AAD and stored byte format until an
explicit versioned migration is implemented. Changing AAD or losing/changing the
key makes old payloads unreadable. Back up keys separately from encrypted data.

This primitive is not a key-management service, Mongo field-level encryption,
protection for query metadata, or authentication/authorization of the caller.
Never log keys, plaintext, ciphertext or raw storage errors. Storage and access
decisions remain the responsibility of the owning domain.

Tests cover legacy standard-library byte compatibility, exact plaintext bytes,
key ownership, fresh nonces, wrong keys/identities, tampering and truncation.
