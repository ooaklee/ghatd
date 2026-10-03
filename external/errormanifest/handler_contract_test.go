package errormanifest_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/policy"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/seo"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/vision"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// mappedHTTPHandler exposes the common boundary while retaining domain-owned
// constructors, dependency maps and existing public reply factory signatures.
type mappedHTTPHandler interface {
	NewHTTPErrorResponse(http.ResponseWriter, error, ...reply.ResponseAttributes) error
}

// TestDomainHandlerManifestResponses compares every migrated public domain key
// with reply's direct-error contract, then verifies the wrapped equivalent.
func TestDomainHandlerManifestResponses(t *testing.T) {
	for _, domain := range []struct {
		name     string
		handler  mappedHTTPHandler
		manifest reply.ErrorManifest
	}{
		{"billing manager", &billingmanager.Handler{}, billingmanager.BillingManagerErrorMap},
		{"contact", &contacter.Handler{}, contacter.ContacterErrorMap},
		{"content manager", &contentmanager.Handler{}, contentmanager.ContentManagerErrorMap},
		{"group", &group.Handler{}, group.GroupErrorMap},
		{"policy", &policy.Handler{}, policy.PolicyErrorMap},
		{"pricing", &pricer.Handler{}, pricer.PricerErrorMap},
		{"sitemap", &seo.Handler{}, seo.SitemapErrorMap},
		{"user", &user.Handler{}, user.UserErrorMap},
		{"user manager", &usermanager.Handler{}, usermanager.UsermanagerErrorMap},
		{"vision", &vision.Handler{}, vision.VisionErrorMap},
	} {
		t.Run(domain.name, func(t *testing.T) {
			for key, item := range domain.manifest {
				t.Run(key.Error(), func(t *testing.T) {
					for _, wrapped := range []bool{false, true} {
						t.Run(fmt.Sprint("wrapped=", wrapped), func(t *testing.T) {
							err := key
							if wrapped {
								err = fmt.Errorf("private diagnostic: %w", key)
							}
							want, got := httptest.NewRecorder(), httptest.NewRecorder()
							require.NoError(t, reply.NewReplier([]reply.ErrorManifest{domain.manifest}).NewHTTPErrorResponse(want, key))
							require.NoError(t, domain.handler.NewHTTPErrorResponse(got, err))
							require.Equal(t, want.Code, got.Code)
							require.Equal(t, want.Header(), got.Header())
							require.Equal(t, want.Body.String(), got.Body.String())
							require.NotEmpty(t, item.Code, "every expected failure needs a stable client code")
						})
					}
				})
			}
		})
	}
}

// TestDeclaredDomainSentinelsHaveManifestEntries guards mapping completeness for
// conventional domain declarations, with explicit cross-domain ownership checks.
func TestDeclaredDomainSentinelsHaveManifestEntries(t *testing.T) {
	for _, path := range []string{
		"../../internal/blueprint", "../contacter", "../usermanager", "../group", "../policy", "../pricer",
		"../seo", "../vision", "../billingmanager", "../contentmanager", "../user/v2",
	} {
		t.Run(path, func(t *testing.T) {
			// Source-level convention check complements runtime response tests. New
			// exported errors.New sentinels must be mapped or explicitly classified
			// elsewhere. This is scoped to the migrated domain packages, not a
			// claim that all runtime/native error paths have been audited.
			packages, err := parser.ParseDir(token.NewFileSet(), path, func(info os.FileInfo) bool { return !strings.HasSuffix(info.Name(), "_test.go") }, 0)
			require.NoError(t, err)
			declared, mapped := map[string]bool{}, map[string]bool{}
			for _, pkg := range packages {
				for _, file := range pkg.Files {
					for _, declaration := range file.Decls {
						general, ok := declaration.(*ast.GenDecl)
						if !ok || general.Tok != token.VAR {
							continue
						}
						for _, spec := range general.Specs {
							value, ok := spec.(*ast.ValueSpec)
							if !ok {
								continue
							}
							for i, expression := range value.Values {
								if call, ok := expression.(*ast.CallExpr); ok && len(value.Names) == len(value.Values) {
									if fn, ok := call.Fun.(*ast.SelectorExpr); ok {
										if pkg, ok := fn.X.(*ast.Ident); ok && pkg.Name == "errors" && fn.Sel.Name == "New" && strings.HasPrefix(value.Names[i].Name, "Err") {
											declared[value.Names[i].Name] = true
										}
									}
								}
								literal, ok := expression.(*ast.CompositeLit)
								if !ok {
									continue
								}
								typ, ok := literal.Type.(*ast.SelectorExpr)
								if !ok || typ.Sel.Name != "ErrorManifest" {
									continue
								}
								for _, entry := range literal.Elts {
									if kv, ok := entry.(*ast.KeyValueExpr); ok {
										if key, ok := kv.Key.(*ast.Ident); ok {
											mapped[key.Name] = true
										}
									}
								}
							}
						}
					}
				}
			}
			// OAuth outcomes belong to Access Manager's public contract, not the
			// standalone user handler. Prove those mappings rather than suppressing
			// the seven exceptions by name alone.
			if path == "../user/v2" {
				for name, key := range map[string]error{
					"ErrOAuthReplacementEmailRequired": user.ErrOAuthReplacementEmailRequired,
					"ErrOAuthConnectionConflict":       user.ErrOAuthConnectionConflict,
					"ErrOAuthLinkRequired":             user.ErrOAuthLinkRequired, "ErrOAuthIdentityConflict": user.ErrOAuthIdentityConflict,
					"ErrOAuthRestricted": user.ErrOAuthRestricted, "ErrOAuthUnsupported": user.ErrOAuthUnsupported,
					"ErrOAuthIndexesRequired": user.ErrOAuthIndexesRequired,
				} {
					_, exists := accessmanager.AccessmanagerErrorMap[key]
					require.True(t, exists, name)
					mapped[name] = true
				}
			}
			require.NotEmpty(t, declared)
			for name := range declared {
				require.True(t, mapped[name], "%s needs a manifest entry or an explicit tested translation", name)
			}
		})
	}
}
