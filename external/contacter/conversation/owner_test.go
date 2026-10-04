package commsconversation

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/reply/v2"
)

// Context fixtures test transport composition, not signed-session verification.
// The frozen-candidate HTTP probe supplies the real auth/Redis/Mongo evidence.
func ownerContext(id string) context.Context {
	ctx := helpers.TransitWith(context.Background(), id)
	ctx = helpers.TransitAuthenticatedWith(ctx, true)
	return helpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: id, AccessUUID: "test-session", TokenUse: auth.TokenUseAccess})
}

func TestOwnerGuardRejectsBeforeReadingBodyOrCallingHandler(t *testing.T) {
	for _, test := range []struct {
		name   string
		header http.Header
		ctx    context.Context
		status int
	}{
		{"missing", nil, ownerContext("owner"), 428},
		{"empty", http.Header{OwnerHeader: {""}}, ownerContext("owner"), 400},
		{"duplicate", http.Header{OwnerHeader: {"owner", "owner"}}, ownerContext("owner"), 400},
		{"different header casing", http.Header{OwnerHeader: {"owner"}, strings.ToLower(OwnerHeader): {"owner"}}, ownerContext("owner"), 400},
		{"comma", http.Header{OwnerHeader: {"owner,owner"}}, ownerContext("owner"), 400},
		{"space", http.Header{OwnerHeader: {" owner"}}, ownerContext("owner"), 400},
		{"control", http.Header{OwnerHeader: {"owner\t"}}, ownerContext("owner"), 400},
		{"non ASCII", http.Header{OwnerHeader: {"ownér"}}, ownerContext("owner"), 400},
		{"oversize", http.Header{OwnerHeader: {strings.Repeat("x", 129)}}, ownerContext("owner"), 400},
		{"anonymous", http.Header{OwnerHeader: {"owner"}}, context.Background(), 401},
		{"authenticated ID without session", http.Header{OwnerHeader: {"owner"}}, helpers.TransitAuthenticatedWith(helpers.TransitWith(context.Background(), "owner"), true), 401},
		{"session without access UUID", http.Header{OwnerHeader: {"owner"}}, helpers.TransitSessionWith(ownerContext("owner"), &auth.TokenAccessDetails{UserID: "owner", TokenUse: auth.TokenUseAccess}), 401},
		{"different session owner", http.Header{OwnerHeader: {"owner"}}, ownerContext("another-owner"), 412},
		{"inconsistent snapshot", http.Header{OwnerHeader: {"owner"}}, helpers.TransitWith(ownerContext("owner"), "another-owner"), 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil).WithContext(test.ctx)
			req.Header = test.header
			req.Body = &unreadableBody{t: t}
			w := httptest.NewRecorder()
			requireOwner(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("rejected command reached handler") })).ServeHTTP(w, req)
			if w.Code != test.status {
				t.Fatalf("status = %d, want %d; %s", w.Code, test.status, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private denial is cacheable")
			}
			if strings.Contains(w.Body.String(), "another-owner") || strings.Contains(w.Body.String(), "test-session") {
				t.Fatal("private identity leaked")
			}
			if test.status == 412 && !strings.Contains(w.Body.String(), OwnerChangedCode) {
				t.Fatal("missing exact owner-change code")
			}
		})
	}
}

type unreadableBody struct{ t *testing.T }

func (b *unreadableBody) Read([]byte) (int, error) {
	b.t.Fatal("guard read request body")
	return 0, io.EOF
}
func (b *unreadableBody) Close() error { return nil }

func TestAttachPreservesAuthenticationPolicyBodyAndInventory(t *testing.T) {
	r := router.NewRouter(nil, nil)
	var events []string
	if err := r.SetRouteAuthorizer(func(context.Context, *http.Request, router.RouteDefinition) error {
		events = append(events, "policy")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	authenticate := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			events = append(events, "authentication")
			next.ServeHTTP(w, request.WithContext(ownerContext("owner")))
		})
	}
	g := r.NewRouteGroup("/api/v1/ums", router.AdminSession, authenticate)
	g.Handle(router.RouteDefinition{Path: "/comms/{id}/conversation", Methods: []string{http.MethodPost, http.MethodOptions}, Operation: appendOperation}, func(w http.ResponseWriter, request *http.Request) {
		events = append(events, "handler")
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if request.Method == http.MethodPost && string(body) != "original body" {
			t.Fatalf("body = %q", body)
		}
		w.WriteHeader(204)
	})
	before := r.RouteInventory()
	count, err := Attach(r)
	if err != nil || count != 1 {
		t.Fatalf("Attach = %d, %v", count, err)
	}
	if !reflect.DeepEqual(before, r.RouteInventory()) {
		t.Fatal("guard changed policy inventory")
	}
	for _, method := range []string{http.MethodPost, http.MethodOptions} {
		events = nil
		req := httptest.NewRequest(method, "/api/v1/ums/comms/contact/conversation", strings.NewReader("original body"))
		if method == http.MethodPost {
			req.Header.Set(OwnerHeader, "owner")
		}
		w := httptest.NewRecorder()
		r.GetRouter().ServeHTTP(w, req)
		if w.Code != 204 || !reflect.DeepEqual(events, []string{"authentication", "policy", "handler"}) {
			t.Fatalf("%s: status=%d events=%v", method, w.Code, events)
		}
	}
	events = nil
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ums/comms/contact/conversation", nil)
	req.Header.Set(OwnerHeader, "another-owner")
	w := httptest.NewRecorder()
	r.GetRouter().ServeHTTP(w, req)
	if w.Code != 412 || !reflect.DeepEqual(events, []string{"authentication"}) {
		t.Fatalf("mismatch: status=%d events=%v", w.Code, events)
	}
}

func TestOwnerGuardStillDelegatesPolicyDenial(t *testing.T) {
	r := router.NewRouter(nil, nil)
	if err := r.SetRouteAuthorizer(func(context.Context, *http.Request, router.RouteDefinition) error { return router.ErrRouteDenied }); err != nil {
		t.Fatal(err)
	}
	g := r.NewRouteGroup("/api/v1/ums", router.AdminSession, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			next.ServeHTTP(w, request.WithContext(ownerContext("owner")))
		})
	})
	g.Handle(router.RouteDefinition{Path: "/comms/{id}/conversation", Methods: []string{http.MethodPost}, Operation: appendOperation}, func(http.ResponseWriter, *http.Request) { t.Fatal("policy denial reached handler") })
	if _, err := Attach(r); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ums/comms/contact/conversation", nil)
	req.Header.Set(OwnerHeader, "owner")
	w := httptest.NewRecorder()
	r.GetRouter().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("status = %d; %s", w.Code, w.Body.String())
	}
}

func TestAttachAllowsPublishedBaselineWithoutAddingRoutes(t *testing.T) {
	r := router.NewRouter(nil, nil)
	r.GetRouter().HandleFunc("/public", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) }).Methods(http.MethodGet)
	count, err := Attach(r)
	if err != nil || count != 0 {
		t.Fatalf("Attach = %d, %v", count, err)
	}
	w := httptest.NewRecorder()
	r.GetRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/public", nil))
	if w.Code != 202 || len(r.RouteInventory()) != 0 {
		t.Fatal("baseline changed")
	}
}

func TestAttachRejectsChangedOrUntrackedConversationRegistration(t *testing.T) {
	for _, test := range []string{"API-token mode", "operation", "GET without append", "untracked POST", "renamed parameter", "renamed path", "new method", "combined read and append"} {
		t.Run(test, func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			mode, operation, method, target := router.AdminSession, appendOperation, http.MethodPost, "/comms/{id}/conversation"
			if test == "untracked POST" {
				r.GetRouter().HandleFunc(conversationPath, func(http.ResponseWriter, *http.Request) {}).Methods(method)
			} else {
				if test == "API-token mode" {
					mode = router.AdminSessionOrAPI
				}
				if test == "operation" {
					operation = "other.Append"
				}
				if test == "GET without append" {
					method = http.MethodGet
				}
				if test == "renamed parameter" {
					target = "/comms/{contact}/conversation"
				}
				if test == "renamed path" {
					target = "/comms/{id}/history"
				}
				methods := []string{method}
				if test == "new method" {
					methods = append(methods, http.MethodPut)
				}
				if test == "combined read and append" {
					methods = append(methods, http.MethodGet)
				}
				r.NewRouteGroup("/api/v1/ums", mode, func(next http.Handler) http.Handler { return next }).Handle(router.RouteDefinition{Path: target, Operation: operation, Methods: methods}, func(http.ResponseWriter, *http.Request) {})
			}
			if _, err := Attach(r); err == nil {
				t.Fatal("unsafe registration accepted")
			}
		})
	}
}

func TestOwnerErrorCodeCollisionIsRejected(t *testing.T) {
	if err := uniqueOwnerCodes(reply.ErrorManifest{errors.New("shared"): {StatusCode: 412, Code: OwnerChangedCode}}); err == nil {
		t.Fatal("shared error-code collision accepted")
	}
}

func TestRequireRoutesRejectsMissingOrMisdeclaredConversationAPI(t *testing.T) {
	for _, test := range []struct {
		name      string
		get       bool
		post      bool
		access    router.AccessMode
		operation string
		wantErr   bool
	}{
		{"missing API", false, false, router.AdminSession, "usermanager.GetCommsConversation", true},
		{"missing read", false, true, router.AdminSession, "usermanager.GetCommsConversation", true},
		{"missing append", true, false, router.AdminSession, "usermanager.GetCommsConversation", true},
		{"incorrect read operation", true, true, router.AdminSession, "host.GetConversation", true},
		{"shared read and append", true, true, router.AdminSession, "usermanager.GetCommsConversation", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			if err := r.SetRouteAuthorizer(func(context.Context, *http.Request, router.RouteDefinition) error { return nil }); err != nil {
				t.Fatal(err)
			}
			group := r.NewRouteGroup("/api/v1/ums", test.access, func(next http.Handler) http.Handler { return next })
			noop := func(http.ResponseWriter, *http.Request) {}
			group.Handle(router.RouteDefinition{Path: "/comms/{id}", Operation: "usermanager.UpdateComms", Methods: []string{http.MethodPut, http.MethodOptions}}, noop)
			if test.get {
				group.Handle(router.RouteDefinition{Path: "/comms/{id}/conversation", Operation: test.operation, Methods: []string{http.MethodGet, http.MethodOptions}}, noop)
			}
			if test.post {
				group.Handle(router.RouteDefinition{Path: "/comms/{id}/conversation", Operation: appendOperation, Methods: []string{http.MethodPost, http.MethodOptions}}, noop)
			}
			if err := RequireRoutes(r); (err != nil) != test.wantErr {
				t.Fatalf("RequireRoutes error=%v, wantErr=%v", err, test.wantErr)
			}
		})
	}
}
