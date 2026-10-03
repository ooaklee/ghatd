package pricer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ritwickdey/querydecoder"
	"github.com/stretchr/testify/require"
)

const priceActorID = "66a5ade0-d1d3-4b2c-a735-dedb1eb73e19"
const priceTargetID = "984aa1db-9c57-46bd-866b-b755e055ee27"

// priceMapper adapts real production mappers without replacing their validator
// or JSON/query codecs. Each case constructs its own request body and context.
func priceMapper[T any](fn func(*http.Request, pricer.PricerValidator) (*T, error)) func(*http.Request) (any, error) {
	return func(r *http.Request) (any, error) { return fn(r, validator.NewValidator()) }
}

func TestPriceMutationActorMappers(t *testing.T) {
	for _, route := range []struct {
		name       string
		mapRequest func(*http.Request) (any, error)
	}{
		{"create plan", priceMapper(pricer.MapRequestToCreatePricePlanRequest)},
		{"update plan", priceMapper(pricer.MapRequestToUpdatePricePlanRequest)},
		{"publish plan", priceMapper(pricer.MapRequestToPublishPricePlanRequest)},
		{"archive plan", priceMapper(pricer.MapRequestToArchivePricePlanRequest)},
		{"delete plan", priceMapper(pricer.MapRequestToDeletePricePlanRequest)},
		{"create feature", priceMapper(pricer.MapRequestToCreateFeatureRequest)},
		{"update feature", priceMapper(pricer.MapRequestToUpdateFeatureRequest)},
		{"delete feature", priceMapper(pricer.MapRequestToDeleteFeatureRequest)},
	} {
		t.Run(route.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, actor         string
				authenticated, flag bool
			}{
				{"authenticated", priceActorID, true, true},
				{"anonymous placeholder", priceActorID, false, true},
				{"ID without authentication", priceActorID, false, false},
				{"flag without ID", "", true, true},
				{"missing identity", "", false, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx := accesshelpers.TransitWith(context.Background(), tc.actor)
					if tc.flag {
						ctx = accesshelpers.TransitAuthenticatedWith(ctx, tc.authenticated)
					}
					r := httptest.NewRequest(http.MethodPost, "/?ActorID=forged&actor_id=forged&-=forged&id=forged", strings.NewReader(`{"name":"Plan","type":"boolean","id":"forged","ActorID":"forged","actorid":"forged","actor_id":null,"UserID":"forged"}`)).WithContext(ctx)
					r = mux.SetURLVars(r, map[string]string{"id": priceTargetID})
					got, err := route.mapRequest(r)
					if !tc.authenticated || tc.actor == "" {
						require.ErrorIs(t, err, pricer.ErrPriceUserIDRequired)
						return
					}
					require.NoError(t, err)
					value := reflect.ValueOf(got).Elem()
					require.Equal(t, priceActorID, value.FieldByName("ActorID").String())
					if target := value.FieldByName("ID"); target.IsValid() {
						require.Equal(t, priceTargetID, target.String())
					}
					assertPriceActorCodec(t, value.Type())
				})
			}
			for _, tc := range []struct {
				name    string
				request *http.Request
			}{
				{"nil request", nil}, {"nil URL", &http.Request{}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					_, err := route.mapRequest(tc.request)
					require.ErrorIs(t, err, pricer.ErrInvalidPriceQueryParam)
				})
			}
		})
	}
}

// assertPriceActorCodec pins the actual decoder's ignore behavior; a query dash
// tag is deliberately absent because the library treats it as an input key.
func assertPriceActorCodec(t *testing.T, typ reflect.Type) {
	t.Helper()
	field, found := typ.FieldByName("ActorID")
	require.True(t, found)
	require.Len(t, field.Index, 1)
	require.Equal(t, "-", field.Tag.Get("json"))
	require.Empty(t, field.Tag.Get("query"))
	require.Empty(t, field.Tag.Get("path"))
	for _, body := range []string{`{"ActorID":"forged","actorid":"forged","actor_id":"forged","UserID":"forged"}`, `{"ActorID":null}`} {
		value := reflect.New(typ)
		value.Elem().FieldByName("ActorID").SetString(priceActorID)
		require.NoError(t, json.Unmarshal([]byte(body), value.Interface()))
		require.NoError(t, querydecoder.New(url.Values{"ActorID": {"forged"}, "actor_id": {"forged"}, "-": {"forged"}}).Decode(value.Interface()))
		require.Equal(t, priceActorID, value.Elem().FieldByName("ActorID").String())
		encoded, err := json.Marshal(value.Interface())
		require.NoError(t, err)
		require.NotContains(t, string(encoded), priceActorID)
	}
}

func TestPriceHTTPReplacementAndTargets(t *testing.T) {
	for _, route := range []struct {
		name, key  string
		invalid    error
		mapRequest func(*http.Request) (any, error)
	}{
		{"plan", "price_plan", pricer.ErrInvalidPricePlanPayload, priceMapper(pricer.MapRequestToUpdatePricePlanRequest)},
		{"feature", "feature", pricer.ErrInvalidPriceFeaturePayload, priceMapper(pricer.MapRequestToUpdateFeatureRequest)},
	} {
		t.Run(route.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, body string
				want       error
			}{
				{"replacement", `{"` + route.key + `":{"id":"` + priceTargetID + `"}}`, route.invalid},
				{"case folded replacement", `{"` + strings.ToUpper(route.key) + `":{"id":"other"}}`, route.invalid},
				{"null replacement is field edit", `{"` + route.key + `":null,"name":"Updated"}`, nil},
				{"body target cannot replace route", `{"id":"different","name":"Updated"}`, nil},
				{"invalid JSON", `{`, route.invalid},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx := accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(context.Background(), priceActorID), true)
					r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(tc.body)).WithContext(ctx)
					r = mux.SetURLVars(r, map[string]string{"id": priceTargetID})
					got, err := route.mapRequest(r)
					if tc.want != nil {
						require.ErrorIs(t, err, tc.want)
						return
					}
					require.NoError(t, err)
					require.Equal(t, priceTargetID, reflect.ValueOf(got).Elem().FieldByName("ID").String())
				})
			}
		})
	}
}
