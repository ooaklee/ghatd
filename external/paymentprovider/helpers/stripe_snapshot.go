package helpers

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

const stripeRetainedSnapshotLimit int64 = 2 << 20

// ReadRetainedStripeRefundSnapshotFile reads a nonempty regular file of at most
// 2 MiB with no group/other permission bits. It screens the charge.refunded/charge
// envelope and returns the exact original bytes. This is input screening, not
// signature verification or proof of a payment/refund. The owning financial
// service must verify retained evidence, original fingerprint and current scope.
//
// Shape, size and permission failures return ErrStripeRetainedSnapshotInvalid;
// filesystem causes pass through. Close failure withholds bytes and retains all
// causes. Hosts own path selection, diagnostic redaction and financial mapping.
func ReadRetainedStripeRefundSnapshotFile(path string) (data []byte, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			data = nil
			err = errors.Join(err, closeErr)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() <= 0 || info.Size() > stripeRetainedSnapshotLimit {
		return nil, ErrStripeRetainedSnapshotInvalid
	}
	data, err = io.ReadAll(io.LimitReader(file, stripeRetainedSnapshotLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || int64(len(data)) > stripeRetainedSnapshotLimit {
		return nil, ErrStripeRetainedSnapshotInvalid
	}
	var event struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				Object string `json:"object"`
			} `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &event) != nil || event.Type != "charge.refunded" || event.Data.Object.Object != "charge" {
		return nil, ErrStripeRetainedSnapshotInvalid
	}
	return data, nil
}
