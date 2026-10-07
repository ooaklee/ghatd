package recordstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestRecordValidation(t *testing.T) {
	cases := []struct {
		name, field string
		size        int
		want        error
	}{
		{"valid identifiers", "", 0, nil}, {"maximum id", "id", 256, nil}, {"oversized id", "id", 257, ErrInvalid},
		{"maximum kind", "kind", 64, nil}, {"oversized kind", "kind", 65, ErrInvalid},
		{"maximum partition", "partition", 256, nil}, {"oversized partition", "partition", 257, ErrInvalid},
		{"empty id", "id", 0, ErrInvalid}, {"empty kind", "kind", 0, ErrInvalid}, {"empty partition", "partition", 0, ErrInvalid},
		{"negative sequence", "sequence", -1, ErrInvalid}, {"maximum state", "state", 256, nil}, {"oversized state", "state", 257, ErrInvalid},
		{"zero revision", "revision", 0, ErrInvalid}, {"invalid JSON", "json", 0, ErrInvalid}, {"maximum payload", "payload", 1 << 20, nil}, {"oversized payload", "payload", (1 << 20) + 1, ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Record{ID: "id", Kind: "kind", Partition: "partition", Revision: 1, Data: json.RawMessage(`{"value":"fixture"}`)}
			switch tc.field {
			case "id":
				r.ID = strings.Repeat("a", tc.size)
			case "kind":
				r.Kind = strings.Repeat("a", tc.size)
			case "partition":
				r.Partition = strings.Repeat("a", tc.size)
			case "state":
				r.State = strings.Repeat("a", tc.size)
			case "sequence":
				r.Sequence = int64(tc.size)
			case "revision":
				r.Revision = int64(tc.size)
			case "json":
				r.Data = []byte("{")
			case "payload":
				r.Data = []byte(`"` + strings.Repeat("a", tc.size-2) + `"`)
			}
			require.ErrorIs(t, validate(r), tc.want)
		})
	}
}
func TestRecordAuthenticatedIdentity(t *testing.T) {
	cases := []struct {
		name, field string
		want        error
	}{
		{"unchanged", "", nil}, {"different kind", "kind", ErrUnavailable}, {"different id", "id", ErrUnavailable},
		{"different partition", "partition", encryption.ErrInvalidPayload}, {"different revision", "revision", encryption.ErrInvalidPayload},
		{"different sequence", "sequence", encryption.ErrInvalidPayload}, {"different state", "state", encryption.ErrInvalidPayload}, {"tampered cipher text", "cipher", encryption.ErrInvalidPayload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cipher, err := encryption.NewPayloadCipher([]byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			s := &MongoStore{cipher: cipher}
			r, err := NewRecord("kind", "id", "partition", 1, map[string]string{"private": "fixture"})
			require.NoError(t, err)
			r.Sequence = 3
			r.State = "processing"
			stored, err := s.encode(r)
			require.NoError(t, err)
			require.NotContains(t, string(stored.Payload), "fixture")
			switch tc.field {
			case "kind":
				stored.Kind = "another-kind"
			case "id":
				stored.ID = "another-id"
			case "partition":
				stored.Partition = "another-partition"
			case "revision":
				stored.Revision++
			case "sequence":
				stored.Sequence++
			case "state":
				stored.State = "paid"
			case "cipher":
				stored.Payload[len(stored.Payload)-1] ^= 1
			}
			out, err := s.decode(stored)
			require.ErrorIs(t, err, tc.want)
			if err == nil {
				require.Equal(t, r, out)
			} else {
				require.Nil(t, out.Data)
			}
		})
	}
}

type pagesTx struct {
	Tx
	rows   []Record
	calls  int
	failAt int
	stall  bool
	cancel context.CancelFunc
}

var pageFailure = errors.New("page-outage")

func (p *pagesTx) Find(_ context.Context, q Query) ([]Record, error) {
	p.calls++
	if p.calls == p.failAt {
		return nil, pageFailure
	}
	start := 0
	if !p.stall {
		for start < len(p.rows) && p.rows[start].ID <= q.AfterID {
			start++
		}
	}
	end := start + q.Limit
	if end > len(p.rows) {
		end = len(p.rows)
	}
	if p.cancel != nil && p.calls == 2 {
		p.cancel()
	}
	return append([]Record(nil), p.rows[start:end]...), nil
}
func TestFindAllCompleteOrError(t *testing.T) {
	cases := []struct {
		name            string
		count, failAt   int
		stall, canceled bool
		want            error
	}{
		{"empty", 0, 0, false, false, nil}, {"below one page", 199, 0, false, false, nil}, {"exact page", 200, 0, false, false, nil}, {"page boundary", 201, 0, false, false, nil}, {"multiple pages", 401, 0, false, false, nil},
		{"later page outage", 401, 2, false, false, pageFailure}, {"stalled continuation", 401, 0, true, false, ErrUnavailable}, {"cancelled between pages", 401, 0, false, true, context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &pagesTx{failAt: tc.failAt, stall: tc.stall}
			if tc.canceled {
				p.cancel = cancel
			}
			for i := 0; i < tc.count; i++ {
				p.rows = append(p.rows, Record{ID: fmt.Sprintf("%06d", i)})
			}
			rows, err := FindAll(ctx, p, Query{Kind: "journal", Partition: "ledger"})
			require.ErrorIs(t, err, tc.want)
			if err == nil {
				require.Len(t, rows, tc.count)
			} else {
				require.Nil(t, rows)
			}
		})
	}
}
func TestAbsenceDoesNotHideOutages(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"native absence", mongo.ErrNoDocuments, true}, {"wrapped absence", fmt.Errorf("context: %w", mongo.ErrNoDocuments), true}, {"joined outage", errors.Join(mongo.ErrNoDocuments, pageFailure), false}, {"outage", pageFailure, false}, {"success", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, absent(tc.err)) })
	}
}
func TestReadOnlySnapshotRejectsMutations(t *testing.T) {
	cases := []struct {
		name    string
		replace bool
	}{{"insert", false}, {"replace", true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := &mongoTx{readOnly: true}
			r, err := NewRecord("kind", "id", "partition", 2, struct{}{})
			require.NoError(t, err)
			if tc.replace {
				err = tx.Replace(context.Background(), r, 1)
			} else {
				err = tx.Insert(context.Background(), r)
			}
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}
