package billingmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/pricer"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
	"github.com/ritwickdey/querydecoder"
	"github.com/stretchr/testify/require"
)

// billingActorMapper keeps each production mapper in the same identity matrix.
func billingActorMapper[T any](fn func(*http.Request, BillingManagerValidator) (*T, error)) func(*http.Request) (any, error) {
	return func(r *http.Request) (any, error) { return fn(r, nil) }
}

func TestBillingActorMappers(t *testing.T) {
	for _, route := range []struct {
		name       string
		public     bool
		mapRequest func(*http.Request) (any, error)
	}{
		{"checkout", false, billingActorMapper(mapRequestToProcessBillingProviderCheckoutRequest)},
		{"portal", false, billingActorMapper(mapRequestToProcessBillingProviderPortalRequest)},
		{"subscription", false, billingActorMapper(mapRequestToGetUserSubscriptionStatusRequest)},
		{"events", false, billingActorMapper(mapRequestToGetUserBillingEventsRequest)},
		{"detail", false, billingActorMapper(mapRequestToGetUserBillingDetailRequest)},
		{"plans", true, billingActorMapper(MapRequestToGetPricingPlansRequest)},
		{"plan", true, billingActorMapper(MapRequestToGetPricePlanBySlugRequest)},
		{"features", true, billingActorMapper(MapRequestToGetPriceFeaturesRequest)},
	} {
		t.Run(route.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, id            string
				authenticated, flag bool
			}{
				{"verified caller", "actor", true, true},
				{"anonymous placeholder", "admin", false, true},
				{"ID only", "admin", false, false},
				{"flag only", "", true, true},
				{"no identity", "", false, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx := accesshelpers.TransitWith(context.Background(), tc.id)
					if tc.flag {
						ctx = accesshelpers.TransitAuthenticatedWith(ctx, tc.authenticated)
					}
					r := billingActorRequest(ctx)
					got, err := route.mapRequest(r)
					wantActor := ""
					if tc.authenticated {
						wantActor = tc.id
					}
					if !route.public && wantActor == "" {
						require.ErrorIs(t, err, ErrBillingManagerUnableToIdentifyUser)
						return
					}
					require.NoError(t, err)
					value := reflect.ValueOf(got).Elem()
					require.Equal(t, wantActor, value.FieldByName("ActorID").String())
					if field := value.FieldByName("UserID"); field.IsValid() {
						require.Equal(t, "target", field.String(), "target remains route-owned")
					}
					assertBillingActorCodec(t, value.Type())
				})
			}
		})
	}
}

// billingActorRequest attempts to forge identity through each transport channel.
func billingActorRequest(ctx context.Context) *http.Request {
	r := httptest.NewRequest(http.MethodPost,
		"/?price=price_example&ActorID=forged&actorid=forged&actor_id=forged&UserID=forged&RequestingUserID=forged&-=forged",
		strings.NewReader(`{"ActorID":"forged","actorid":"forged","actor_id":null,"UserID":"forged","RequestingUserID":"forged"}`)).WithContext(ctx)
	return mux.SetURLVars(r, map[string]string{"providerName": "stripe", "userId": "target", "slug": "starter"})
}

// assertBillingActorCodec exercises real decoding, including querydecoder's
// literal "-" key behavior. An actor must never become a transport field.
func assertBillingActorCodec(t *testing.T, typ reflect.Type) {
	t.Helper()
	field, found := typ.FieldByName("ActorID")
	require.True(t, found)
	require.Len(t, field.Index, 1)
	require.Equal(t, "-", field.Tag.Get("json"))
	require.Empty(t, field.Tag.Get("query"))
	require.Empty(t, field.Tag.Get("path"))
	for _, body := range []string{`{"ActorID":"forged","actorid":"forged","actor_id":"forged"}`, `{"ActorID":null}`} {
		value := reflect.New(typ)
		value.Elem().FieldByName("ActorID").SetString("bound-actor")
		require.NoError(t, json.Unmarshal([]byte(body), value.Interface()))
		require.NoError(t, querydecoder.New(url.Values{"ActorID": {"forged"}, "actor_id": {"forged"}, "-": {"forged"}}).Decode(value.Interface()))
		require.Equal(t, "bound-actor", value.Elem().FieldByName("ActorID").String())
		data, err := json.Marshal(value.Interface())
		require.NoError(t, err)
		require.NotContains(t, string(data), "bound-actor")
	}
}

// billingActorUsers records exactly which account is consulted for authority.
// Other user capabilities panic when unexpectedly invoked.
type billingActorUsers struct {
	UserService
	ids      []string
	result   *user.GetUserByIDResponse
	err      error
	override bool
	cancel   context.CancelFunc
}

func (s *billingActorUsers) GetUserByID(_ context.Context, r *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	s.ids = append(s.ids, r.ID)
	if s.cancel != nil {
		s.cancel()
	}
	if s.override {
		return s.result, s.err
	}
	model := &user.UniversalUser{ID: r.ID}
	if r.ID == "admin" {
		model.Roles = []string{"ADMIN"}
	}
	return &user.GetUserByIDResponse{User: model}, s.err
}

// billingActorStore records target selection after the real manager authorizes
// the actor; it cannot silently accept unrelated writes or fallback lookups.
type billingActorStore struct {
	BillingService
	targets []string
}

func (s *billingActorStore) GetSubscriptions(_ context.Context, r *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
	s.targets = append(s.targets, r.ForUserIDs...)
	return &billing.GetSubscriptionsResponse{Total: 1, Subscriptions: []billing.Subscription{{ID: "sub", UserID: r.ForUserIDs[0], Status: billing.StatusActive}}}, nil
}

func (s *billingActorStore) GetBillingEvents(_ context.Context, r *billing.GetBillingEventsRequest) (*billing.GetBillingEventsResponse, error) {
	s.targets = append(s.targets, r.ForUserIDs...)
	return &billing.GetBillingEventsResponse{}, nil
}

// billingActorRead adapts the three private projections, without replacing
// authorization, lower-domain queries or HTTP error serialization.
func billingActorRead(s *Service, ctx context.Context, operation, actor, target string) error {
	switch operation {
	case "subscription":
		_, err := s.GetUserSubscriptionStatus(ctx, &GetUserSubscriptionStatusRequest{ActorID: actor, UserID: target})
		return err
	case "detail":
		_, err := s.GetUserBillingDetail(ctx, &GetUserBillingDetailRequest{ActorID: actor, UserID: target})
		return err
	default:
		_, err := s.GetUserBillingEvents(ctx, &GetUserBillingEventsRequest{ActorID: actor, UserID: target})
		return err
	}
}

func TestBillingReadsSeparateActorAndTarget(t *testing.T) {
	outage := errors.New("private-diagnostic outage")
	for _, operation := range []string{"subscription", "events", "detail"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range []struct {
				name, actor, target                                      string
				result                                                   *user.GetUserByIDResponse
				override, noUsers, noBilling, cancelBefore, cancelLookup bool
				lookupErr, wantErr                                       error
			}{
				{name: "self without user adapter", actor: "target", target: "target", noUsers: true},
				{name: "admin different target", actor: "admin", target: "target"},
				{name: "member different target", actor: "member", target: "target", wantErr: ErrBillingManagerUserUnauthorisedToCarryOutOperation},
				{name: "admin target confers no authority", actor: "member", target: "admin", wantErr: ErrBillingManagerUserUnauthorisedToCarryOutOperation},
				{name: "empty actor", target: "target", wantErr: ErrBillingManagerUnableToIdentifyUser},
				{name: "blank actor", actor: " ", target: "target", wantErr: ErrBillingManagerUnableToIdentifyUser},
				{name: "empty target", actor: "admin", wantErr: ErrBillingManagerRequiresUserIdIsMissing},
				{name: "nil authority", actor: "admin", target: "target", noUsers: true, wantErr: ErrBillingManagerServiceUnavailable},
				{name: "nil billing", actor: "admin", target: "target", noBilling: true, wantErr: ErrBillingManagerServiceUnavailable},
				{name: "nil result", actor: "admin", target: "target", override: true, wantErr: ErrBillingManagerServiceUnavailable},
				{name: "nil model", actor: "admin", target: "target", override: true, result: &user.GetUserByIDResponse{}, wantErr: ErrBillingManagerServiceUnavailable},
				{name: "wrong admin identity", actor: "member", target: "target", override: true, result: &user.GetUserByIDResponse{User: &user.UniversalUser{ID: "admin", Roles: []string{"ADMIN"}}}, wantErr: ErrBillingManagerServiceUnavailable},
				{name: "native outage", actor: "admin", target: "target", lookupErr: outage, wantErr: outage},
				{name: "wrapped absence", actor: "admin", target: "target", lookupErr: fmt.Errorf("private-diagnostic: %w", user.ErrUserNotFound), wantErr: user.ErrUserNotFound},
				{name: "mixed failure", actor: "admin", target: "target", lookupErr: errors.Join(user.ErrUserNotFound, outage), wantErr: outage},
				{name: "cancel before lookup", actor: "admin", target: "target", cancelBefore: true, wantErr: context.Canceled},
				{name: "cancel in lookup", actor: "admin", target: "target", cancelLookup: true, wantErr: context.Canceled},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					if tc.cancelBefore {
						cancel()
					}
					users := &billingActorUsers{override: tc.override, result: tc.result, err: tc.lookupErr}
					if tc.cancelLookup {
						users.cancel = cancel
					}
					store := &billingActorStore{}
					s := &Service{UserService: users, BillingService: store}
					if tc.noUsers {
						s.UserService = nil
					}
					if tc.noBilling {
						s.BillingService = nil
					}
					err := billingActorRead(s, ctx, operation, tc.actor, tc.target)
					if tc.wantErr != nil {
						require.ErrorIs(t, err, tc.wantErr)
						require.Empty(t, store.targets)
					} else {
						require.NoError(t, err)
						require.Equal(t, []string{tc.target}, store.targets)
					}
					if tc.lookupErr != nil {
						require.Same(t, tc.lookupErr, err, "preserve original error graph")
					}
					for _, id := range users.ids {
						require.Equal(t, tc.actor, id)
					}
					if tc.actor == tc.target || tc.cancelBefore || tc.actor == "" || tc.actor == " " || tc.target == "" || tc.noBilling {
						require.Empty(t, users.ids)
					}
				})
			}
		})
	}
}

func TestBillingReadHandlerIdentityAndErrorMaps(t *testing.T) {
	for _, operation := range []string{"subscription", "events", "detail"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range []struct {
				name, actor             string
				authenticated, override bool
				err                     error
				status                  int
			}{
				{"self", "target", true, false, nil, 200},
				{"admin distinct target", "admin", true, false, nil, 200},
				{"member denied", "member", true, false, nil, 403},
				{"anonymous placeholder", "admin", false, false, nil, 401},
				{"wrapped mapped", "admin", true, false, fmt.Errorf("private-diagnostic: %w", user.ErrUserNotFound), 404},
				{"host override", "admin", true, true, fmt.Errorf("private-diagnostic: %w", user.ErrUserNotFound), 409},
				{"unknown", "admin", true, false, errors.New("private-diagnostic"), 500},
				{"mixed mapped and unknown", "admin", true, false, errors.Join(user.ErrUserNotFound, errors.New("private-diagnostic")), 500},
			} {
				t.Run(tc.name, func(t *testing.T) {
					users, store := &billingActorUsers{err: tc.err}, &billingActorStore{}
					h := NewHandler(&Service{UserService: users, BillingService: store}, nil)
					if tc.override {
						h.ErrorMaps = []reply.ErrorManifest{{user.ErrUserNotFound: {Title: "Conflict", Detail: "Unavailable actor", StatusCode: 409, Code: "host-actor"}}}
					}
					ctx := accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(context.Background(), tc.actor), tc.authenticated)
					recorder := httptest.NewRecorder()
					switch operation {
					case "subscription":
						h.GetUserSubscriptionStatus(recorder, billingActorRequest(ctx))
					case "detail":
						h.GetUserBillingDetail(recorder, billingActorRequest(ctx))
					default:
						h.GetUserBillingEvents(recorder, billingActorRequest(ctx))
					}
					require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
					require.NotContains(t, recorder.Body.String(), "private-diagnostic")
					if tc.status == 200 {
						require.Equal(t, []string{"target"}, store.targets)
					} else {
						require.Empty(t, store.targets)
					}
					if tc.override {
						require.Contains(t, recorder.Body.String(), "host-actor")
					}
				})
			}
		})
	}
}

// billingActorPricer records visibility restrictions applied to catalogue reads.
type billingActorPricer struct {
	plans    *pricer.GetPricePlansRequest
	features *pricer.GetFeaturesRequest
}

func (p *billingActorPricer) GetPricePlans(_ context.Context, r *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error) {
	p.plans = r
	return &pricer.GetPricePlansResponse{}, nil
}
func (p *billingActorPricer) GetFeatures(_ context.Context, r *pricer.GetFeaturesRequest) (*pricer.GetFeaturesResponse, error) {
	p.features = r
	return &pricer.GetFeaturesResponse{}, nil
}
func (p *billingActorPricer) GetPricePlanBySlug(context.Context, *pricer.GetPricePlanBySlugRequest) (*pricer.GetPricePlanBySlugResponse, error) {
	return &pricer.GetPricePlanBySlugResponse{GetPricePlanResponse: &pricer.GetPricePlanResponse{PricePlan: &pricer.PricePlan{Status: pricer.PricePlanStatusDraft}}}, nil
}

func TestBillingPublicPricingUsesOnlyVerifiedActors(t *testing.T) {
	for _, operation := range []string{"plans", "plan", "features"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range []struct {
				name, actor                       string
				authenticated, wrongOwner, outage bool
			}{
				{"anonymous", "", false, false, false},
				{"placeholder matches admin ID", "admin", false, false, false},
				{"member", "member", true, false, false},
				{"admin", "admin", true, false, false},
				{"wrong returned owner", "member", true, true, false},
				{"authority outage", "admin", true, false, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					users, pricing := &billingActorUsers{}, &billingActorPricer{}
					if tc.wrongOwner {
						users.override = true
						users.result = &user.GetUserByIDResponse{User: &user.UniversalUser{ID: "admin", Roles: []string{"ADMIN"}}}
					}
					if tc.outage {
						users.err = errors.New("private-diagnostic")
					}
					h := NewHandler(&Service{UserService: users, PricerService: pricing}, nil)
					ctx := accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(context.Background(), tc.actor), tc.authenticated)
					w := httptest.NewRecorder()
					switch operation {
					case "plans":
						h.GetPricingPlans(w, billingActorRequest(ctx))
					case "plan":
						h.GetPricePlanBySlug(w, billingActorRequest(ctx))
					default:
						h.GetPricingFeatures(w, billingActorRequest(ctx))
					}
					admin := tc.authenticated && tc.actor == "admin" && !tc.outage
					status := 200
					if operation == "plan" && !admin {
						status = 404
					}
					require.Equal(t, status, w.Code, w.Body.String())
					if !tc.authenticated {
						require.Empty(t, users.ids)
					} else {
						require.Equal(t, []string{tc.actor}, users.ids)
					}
					if operation == "plans" {
						require.Equal(t, !admin, pricing.plans.IsPublished)
						require.Equal(t, !admin, pricing.plans.IsNotDeleted)
					}
					if operation == "features" {
						require.Equal(t, !admin, pricing.features.IsPublished)
						require.Equal(t, !admin, pricing.features.IsNotDeleted)
					}
					require.NotContains(t, w.Body.String(), "private-diagnostic")
				})
			}
		})
	}
}
