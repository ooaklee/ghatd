package bundles

import (
	"reflect"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/notifier"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/reminder"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
)

func TestBundles(t *testing.T) {
	tests := []struct {
		name string
		run  func() []reply.ErrorManifest
		want []reply.ErrorManifest
	}{
		{
			name: "Success - access manager bundle matches app wiring",
			run:  AccessManager,
			want: []reply.ErrorManifest{
				user.UserErrorMap,
				auth.AuthErrorMap,
				apitoken.ApitokenErrorMap,
				toolbox.ToolboxErrorMap,
				group.GroupErrorMap,
				billingmanager.BillingManagerErrorMap,
				paymentprovider.PaymentProviderErrorMap,
				billing.BillingErrorMap,
			},
		},
		{
			name: "Success - user manager bundle matches app wiring",
			run:  UserManager,
			want: []reply.ErrorManifest{
				user.UserErrorMap,
				contacter.ContacterErrorMap,
				toolbox.ToolboxErrorMap,
				group.GroupErrorMap,
				notifier.NotifierErrorMap,
				reminder.ReminderErrorMap,
			},
		},
		{
			name: "Success - content manager bundle matches app wiring",
			run:  ContentManager,
			want: []reply.ErrorManifest{
				post.PostErrorMap,
				toolbox.ToolboxErrorMap,
				user.UserErrorMap,
			},
		},
		{
			name: "Success - billing manager bundle matches app wiring",
			run:  BillingManager,
			want: []reply.ErrorManifest{
				pricer.PricerErrorMap,
				paymentprovider.PaymentProviderErrorMap,
				billing.BillingErrorMap,
				toolbox.ToolboxErrorMap,
				user.UserErrorMap,
			},
		},
		{
			name: "Success - auth middleware bundle matches app wiring",
			run:  AuthMiddleware,
			want: []reply.ErrorManifest{
				user.UserErrorMap,
				toolbox.ToolboxErrorMap,
				auth.AuthErrorMap,
				apitoken.ApitokenErrorMap,
				ephemeral.EphemeralStoreErrorMap,
				accessmanager.AccessmanagerErrorMap,
				contacter.ContacterErrorMap,
				notifier.NotifierErrorMap,
				post.PostErrorMap,
				billingmanager.BillingManagerErrorMap,
				paymentprovider.PaymentProviderErrorMap,
				billing.BillingErrorMap,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.run()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected bundle %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestBundlesReturnCopies(t *testing.T) {
	tests := []struct {
		name string
		run  func() []reply.ErrorManifest
	}{
		{name: "Safety - access manager bundle can be mutated by caller without leaking", run: AccessManager},
		{name: "Safety - user manager bundle can be mutated by caller without leaking", run: UserManager},
		{name: "Safety - content manager bundle can be mutated by caller without leaking", run: ContentManager},
		{name: "Safety - billing manager bundle can be mutated by caller without leaking", run: BillingManager},
		{name: "Safety - auth middleware bundle can be mutated by caller without leaking", run: AuthMiddleware},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := tt.run()
			second := tt.run()

			if len(first) == 0 || len(first[0]) == 0 {
				t.Fatal("expected first manifest to contain at least one error")
			}

			var errorKey error
			for err := range first[0] {
				errorKey = err
				break
			}

			original := second[0][errorKey]
			first[0][errorKey] = reply.ErrorManifestItem{Title: "mutated"}

			if reflect.DeepEqual(first[0][errorKey], original) {
				t.Fatal("expected local mutation to change first bundle")
			}
			if !reflect.DeepEqual(second[0][errorKey], original) {
				t.Fatal("expected second bundle to be isolated from first bundle mutation")
			}
		})
	}
}
