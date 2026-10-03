package accesspolicy

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// boundaryStore records dispatch without performing persistence. Embedding the
// contract makes unexpected operations fail loudly instead of silently passing.
type boundaryStore struct {
	Store
	reads  int
	cancel context.CancelFunc
}

func (s *boundaryStore) Read(context.Context, Subject) (Grant, error) {
	s.reads++
	if s.cancel != nil {
		s.cancel()
	}
	return policyFixture(), nil
}

func TestServiceRejectsInvalidContextBeforeDispatch(t *testing.T) {
	for _, operation := range []string{"resolve", "authorize", "replace", "consume", "business", "token limits", "token creation"} {
		t.Run(operation, func(t *testing.T) {
			for _, variant := range []string{"nil service", "nil store", "nil context", "canceled"} {
				t.Run(variant, func(t *testing.T) {
					store := &boundaryStore{}
					service := &Service{store: store}
					ctx, cancel := context.WithCancel(context.Background())
					t.Cleanup(cancel)
					want := ErrConfiguration
					switch variant {
					case "nil service":
						service = nil
					case "nil store":
						service.store = nil
					case "nil context":
						ctx = nil
					case "canceled":
						cancel()
						want = context.Canceled
					}
					called := false
					var err error
					require.NotPanics(t, func() {
						switch operation {
						case "resolve":
							_, err = service.Resolve(ctx, policyFixture().Subject)
						case "authorize":
							err = service.Authorize(ctx, policyFixture().Subject, nil, nil)
						case "replace":
							_, err = service.ReplaceGrant(ctx, policyFixture(), 0)
						case "consume":
							_, err = service.ConsumeAuthorized(ctx, consumptionFixture())
						case "business":
							_, err = service.WithConsumption(ctx, consumptionFixture(), ConsumptionAction{Check: func(context.Context) error { called = true; return nil }, Apply: func(context.Context, Usage) error { called = true; return nil }})
						case "token limits":
							_, err = (TokenPolicy{Service: service, System: "sample"}).TokenLimits(ctx, "user-1")
						case "token creation":
							err = (TokenPolicy{Service: service, System: "sample"}).WithTokenCreation(ctx, "user-1", func(context.Context, TokenLimits) error { called = true; return nil })
						}
					})
					require.ErrorIs(t, err, want)
					require.Zero(t, store.reads)
					require.False(t, called)
				})
			}
		})
	}
}

func TestResolveChecksCancellationAfterRead(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "live context"
		if canceled {
			name = "canceled during read"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			t.Cleanup(cancel)
			store := &boundaryStore{}
			if canceled {
				store.cancel = cancel
			}
			service, err := NewService(store, nil)
			require.NoError(t, err)
			grant, err := service.Resolve(ctx, policyFixture().Subject)
			if canceled {
				require.ErrorIs(t, err, context.Canceled)
				require.Zero(t, grant)
			} else {
				require.NoError(t, err)
				require.Equal(t, policyFixture(), grant)
			}
			require.Equal(t, 1, store.reads)
		})
	}
}
