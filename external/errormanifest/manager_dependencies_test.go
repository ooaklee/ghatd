package errormanifest_test

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/errormanifest/bundles"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// TestManagerDependencyDefaults proves the ordinary constructors need no host
// bundle for their built-in dependencies, while legacy wiring and overrides keep
// exactly the same response contract. Every current dependency key is exercised.
func TestManagerDependencyDefaults(t *testing.T) {
	for _, manager := range []struct {
		name         string
		own          reply.ErrorManifest
		dependencies func() []reply.ErrorManifest
		bundle       func() []reply.ErrorManifest
		newHandler   func([]reply.ErrorManifest) mappedHTTPHandler
	}{
		{"access", accessmanager.AccessmanagerErrorMap, accessmanager.DependencyErrorMaps, bundles.AccessManager, func(m []reply.ErrorManifest) mappedHTTPHandler {
			return accessmanager.NewHandler(&accessmanager.NewHandlerRequest{ErrorMaps: m})
		}},
		{"user", usermanager.UsermanagerErrorMap, usermanager.DependencyErrorMaps, bundles.UserManager, func(m []reply.ErrorManifest) mappedHTTPHandler {
			return usermanager.NewHandler(&usermanager.NewHandlerRequest{ErrorMaps: m})
		}},
		{"content", contentmanager.ContentManagerErrorMap, contentmanager.DependencyErrorMaps, bundles.ContentManager, func(m []reply.ErrorManifest) mappedHTTPHandler { return contentmanager.NewHandler(nil, nil, m...) }},
		{"billing", billingmanager.BillingManagerErrorMap, billingmanager.DependencyErrorMaps, bundles.BillingManager, func(m []reply.ErrorManifest) mappedHTTPHandler { return billingmanager.NewHandler(nil, nil, m...) }},
	} {
		t.Run(manager.name, func(t *testing.T) {
			deps := manager.dependencies()
			require.Equal(t, deps, manager.bundle())
			for _, manifest := range deps {
				for key := range manifest {
					t.Run(key.Error(), func(t *testing.T) {
						for _, mode := range []string{"defaults", "nil map", "legacy bundle", "host override"} {
							t.Run(mode, func(t *testing.T) {
								var supplied []reply.ErrorManifest
								switch mode {
								case "nil map":
									supplied = []reply.ErrorManifest{nil}
								case "legacy bundle":
									supplied = manager.bundle()
								case "host override":
									supplied = []reply.ErrorManifest{{key: {Code: "HOST", StatusCode: 422}}}
								}
								expected := errormanifest.NewComposer().Add(manager.own).Add(deps...).AddOverrides(supplied...).Build()
								want, got := httptest.NewRecorder(), httptest.NewRecorder()
								require.NoError(t, reply.NewReplier(expected).NewHTTPErrorResponse(want, key))
								require.NoError(t, manager.newHandler(supplied).NewHTTPErrorResponse(got, fmt.Errorf("private: %w", key)))
								require.Equal(t, want.Code, got.Code)
								require.Equal(t, want.Header(), got.Header())
								require.Equal(t, want.Body.String(), got.Body.String())
							})
						}
					})
				}
			}
			// Changing a returned inventory must not alter subsequent defaults.
			for _, manifest := range deps {
				for key := range manifest {
					manifest[key] = reply.ErrorManifestItem{Code: "MUTATED"}
				}
			}
			require.Equal(t, manager.bundle(), manager.dependencies())
			require.NotEqual(t, deps, manager.dependencies())
		})
	}
}

// TestCloneManifests checks nil preservation and copied map/slice ownership.
func TestCloneManifests(t *testing.T) {
	for _, tc := range []struct {
		name string
		maps []reply.ErrorManifest
	}{
		{"empty", nil}, {"nil entry", []reply.ErrorManifest{nil}}, {"empty map", []reply.ErrorManifest{{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := errormanifest.CloneManifests(tc.maps...)
			require.Len(t, got, len(tc.maps))
			for i := range got {
				require.Equal(t, tc.maps[i], got[i])
			}
		})
	}
}
