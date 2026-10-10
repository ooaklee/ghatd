# Encrypted owned records

`recordstore` is shared persistence infrastructure, not a domain service. It
borrows an existing Mongo database/pool through GHATD repository helpers and an
operator-supplied `encryption.PayloadCipher`. It never generates or persists a
replacement key. The caller remains responsible for the database client lifecycle.

Each record has a bounded kind/ID/partition, positive revision, optional sequence
and state, and a JSON payload of at most 1 MiB. Indexed metadata must contain no
email, raw evidence, provider reference or other secret. The typed payload is
encrypted with authenticated data bound to kind, ID, partition, revision,
sequence and state, plus explicit expiration when present. Changing metadata, ciphertext or the key fails closed.
Internal fields excluded from public JSON need an explicit encrypted storage
envelope; transport exclusion must not accidentally discard replay evidence.

`EnsureIndexes` creates additive identity, partition, state and sequence indexes
and the guard collection, plus a single-field partial absolute-expiration TTL
index. Conflicting/duplicate existing data is an operator
reconciliation error, never permission to delete history. `Probe` proves an
actual shared snapshot/majority read-write-commit round trip. Perform both before
admitting financial commands. Replicaset transaction support is mandatory.

`Transact` serializes conflicting domain commands using the same stable guard
key. Its callback can repeat on a driver retry and must reset result accumulators,
use only its bound `Tx`, and make no external calls or other side effects. `Read`
uses one transaction snapshot and rejects writes. It cannot silently read a
second repository/client outside that snapshot.

`FindAll` reads complete history using pages of 200 records; it returns an error
and no partial result for a later page failure, cancellation or a stalled cursor.
Callers supply bounded contexts and must design indexed projections when history
becomes too large for their operational deadline. A public response page limit
is not a financial history limit.

Expected absence, conflict, validation, unavailable dependency and uncertain
commit have distinct errors. If the callback succeeded but commit acknowledgement
failed, reconcile the original durable receipt before resubmitting the domain
command. Preserve encrypted records, guards and keys across restores/rollbacks;
removing an index or restarting a process is not a financial recovery strategy.

## Explicit ephemeral record expiration

`Record.ExpiresAt` defaults to nil. Only an owning domain adapter may opt an
approved ephemeral record into physical expiration; financial journals, durable
ownership and replay receipts must remain persistent. `WithExpiration` returns a
copy normalized to UTC and rounded **up** to BSON millisecond precision. The
optional timestamp is authenticated alongside existing record metadata. Records
without it keep the original authenticated-data encoding byte for byte.

The additive `owned_record_expiration` index contains only `expires_at`, uses
`expireAfterSeconds: 0`, and applies only to date-valued metadata. Existing records
without that field do not expire. Mongo cleanup is asynchronous and can lag the
chosen instant; it is never consent, authorization, nonce validity or command
admission authority. Reads may still return a physically present expired row.
See [MongoDB TTL behavior](https://www.mongodb.com/docs/manual/core/index-ttl/).

Hosts must prepare the index before collection, approve which domain records can
expire, monitor cleanup and retain persistent aggregate projections before raw
cleanup if reporting needs history. This store does not invent a retention
policy or archive financial data.
