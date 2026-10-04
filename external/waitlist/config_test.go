package waitlist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestSignupConfigDefaults(t *testing.T) {
	for _, tc := range []struct{ name, message, wantMessage string }{
		{"default", "", "Email me when early access is available."},
		{"host copy", "Notify me about early access.", "Notify me about early access."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := (SignupConfig{Message: tc.message}).defaults()
			require.Equal(t, tc.wantMessage, config.Message)
		})
	}
}

func TestSignupCopyDoesNotChangeStableIdentity(t *testing.T) {
	for _, message := range []string{"", "New host copy"} {
		name := message
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			ctx, repository, audience, collection := commsTestDatabase(t)
			id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("ghatd:waitlist:prerelease-v1:person@example.invalid")).String()
			_, err := collection.InsertOne(ctx, bson.M{"_id": id, "type": CommsType, "email": "person@example.invalid", "message": "Original signup", "admin_notes": "Private note"})
			require.NoError(t, err)
			config := SignupConfig{Message: message}
			for range 2 {
				service := NewCommsService(repository, audience)
				legacy := NewCommsSignupStoreWithConfig(service, audience, config)
				require.NoError(t, legacy.Join(ctx, " Person@Example.invalid "))
			}
			count, err := collection.CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			var stored bson.M
			require.NoError(t, collection.FindOne(ctx, bson.M{"_id": id}).Decode(&stored))
			require.Equal(t, "Original signup", stored["message"])
			require.Equal(t, "Private note", stored["admin_notes"])
			entries, err := audience.Export(ctx)
			require.NoError(t, err)
			require.Len(t, entries, 1)
		})
	}
}

func TestWaitlistExportFilenameConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, filename, want string
		valid                bool
	}{
		{"default", "", "prerelease-waitlist.csv", true},
		{"custom", "example-prerelease.csv", "example-prerelease.csv", true},
		{"path", "../private.csv", "", false},
		{"header injection", "list.csv\r\nX-Other: bad", "", false},
		{"quote", "list\".csv", "", false},
		{"wrong extension", "list.html", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			pass := func(next http.Handler) http.Handler { return next }
			err := AttachRoutesWithConfig(r, &fakeStore{}, pass, pass, RouteConfig{ExportFilename: tc.filename})
			if !tc.valid {
				require.Error(t, err)
				require.Empty(t, r.RouteInventory())
				return
			}
			require.NoError(t, err)
			w := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/waitlist/export", nil))
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, `attachment; filename="`+tc.want+`"`, w.Header().Get("Content-Disposition"))
		})
	}
}

func TestAnnouncementBrandEscaping(t *testing.T) {
	for _, tc := range []struct{ name, brand, want string }{
		{"neutral default", "", "Early access"},
		{"host brand", "Example", "Example"},
		{"escaped", "<script>brand</script>", "&lt;script&gt;brand&lt;/script&gt;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, provider := announcementFixture(1)
			s.BrandName = tc.brand
			preview, err := s.Prepare(context.Background(), announcementDraft())
			require.NoError(t, err)
			require.Contains(t, preview.HTML, tc.want)
			require.NotContains(t, preview.HTML, "<script>")
			require.Zero(t, provider.count())
		})
	}
}
