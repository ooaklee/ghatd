package revenuestore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type inspectionReadError struct {
	billing.CheckoutRepository
	err error
}

func (r inspectionReadError) ReadCheckout(context.Context, func(billing.CheckoutTx) error) error {
	return r.err
}

func TestMongoCheckoutInspectionReadOnlyStates(t *testing.T) {
	cases := []struct {
		name, reverse, intent, ack, terms, metadata, agree string
		want                                               error
	}{
		{"binding_without_metadata", "present", "present", "match", "match", "not_supplied", "not_comparable", nil},
		{"no_records", "missing", "not_located", "not_located", "not_checked", "not_supplied", "not_comparable", nil},
		{"missing_intent", "present", "missing", "not_located", "not_checked", "not_supplied", "not_comparable", nil},
		{"missing_forward_ack", "present", "present", "missing", "match", "not_supplied", "not_comparable", nil},
		{"metadata_unknown", "present", "present", "match", "match", "missing", "conflict", nil},
		{"metadata_matches", "present", "present", "match", "match", "present", "match", nil},
		{"terms_mismatch", "present", "present", "match", "mismatch", "not_supplied", "not_comparable", nil},
		{"wrong_cipher", "", "", "", "", "", "", encryption.ErrInvalidPayload},
		{"joined_absence_outage", "", "", "", "", "", "", billing.ErrRevenueNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, db, ctx := revenueFixture(t)
			p := &checkoutEvidenceFixture{}
			svc := checkoutServiceFixture(t, repo, p)
			scope := checkoutResolverRequest().Scope
			intent, err := svc.PrepareCheckout(ctx, scope, historicalCheckoutRequest())
			require.NoError(t, err)
			e := checkoutEvidence(intent)
			e.IntentID = ""
			if tc.name != "no_records" {
				require.NoError(t, svc.AcknowledgeCheckout(ctx, intent, e.SessionID))
			}
			switch tc.name {
			case "no_records":
				_, err = db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": kindCheckoutIntent})
				require.NoError(t, err)
			case "missing_intent":
				_, err = db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": kindCheckoutIntent})
				require.NoError(t, err)
			case "missing_forward_ack":
				_, err = db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": kindCheckoutAck})
				require.NoError(t, err)
			case "metadata_unknown":
				e.IntentID = "unknown_original"
			case "metadata_matches":
				e.IntentID = intent.ID
			case "terms_mismatch":
				e.UnitAmountMinor++
			case "wrong_cipher":
				cipher, err := encryption.NewPayloadCipher([]byte("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"))
				require.NoError(t, err)
				store, err := recordstore.NewMongoStoreFromDatabase(db, cipher)
				require.NoError(t, err)
				bad, err := NewRepository(store)
				require.NoError(t, err)
				svc = checkoutServiceFixture(t, bad, p)
			case "joined_absence_outage":
				svc = checkoutServiceFixture(t, inspectionReadError{repo, errors.Join(billing.ErrRevenueNotFound, billing.ErrRevenueUnavailable)}, p)
			}
			snapshot := func() map[string][]bson.M {
				out := map[string][]bson.M{}
				for _, name := range []string{"ghatd_owned_records", "ghatd_record_guards"} {
					cur, err := db.Collection(name).Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
					require.NoError(t, err)
					var rows []bson.M
					require.NoError(t, cur.All(ctx, &rows))
					out[name] = rows
				}
				return out
			}
			before := snapshot()
			out, err := svc.InspectCheckoutBinding(ctx, e)
			require.Equal(t, before, snapshot())
			require.Zero(t, p.calls)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, billing.CheckoutBindingInspection{}, out)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.reverse, out.ReverseRecord)
			require.Equal(t, tc.intent, out.ReverseIntent)
			require.Equal(t, tc.ack, out.ReverseAcknowledgement)
			require.Equal(t, tc.terms, out.ReverseTerms)
			require.Equal(t, tc.metadata, out.MetadataIntent)
			require.Equal(t, tc.agree, out.ReferencesAgree)
			raw, err := json.Marshal(out)
			require.NoError(t, err)
			for _, private := range []string{intent.ID, "cs_original", "owning_principal", "original@example.test"} {
				require.NotContains(t, string(raw), private)
			}
		})
	}
}
