package teleprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const session = "11111111-1111-4111-8111-111111111111"
const phone = "+12025550123"
const jid = "12025550123@c.us"
const group = "120363000000000000@g.us"
const registered = `{"number":"12025550123","exists":true,"whatsappId":"12025550123@c.us"}`
const absent = `{"number":"12025550123","exists":false,"whatsappId":null}`

// configured creates an isolated OpenWA-backed service and registers server cleanup with the test.
func configured(t *testing.T, handler http.HandlerFunc, change func(*Config)) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := Config{Provider: "openwa", OpenWA: OpenWAConfig{Endpoint: server.URL + "/api/", APIKey: "test-only-key", SessionID: session}}
	if change != nil {
		change(&c)
	}
	s, err := NewTeleProvider(c)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// requireCode asserts the safe public classification and returns the typed error for recovery checks.
func requireCode(t *testing.T, err error, code string) *Error {
	t.Helper()
	var got *Error
	if !errors.As(err, &got) || got.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
	if err.Error() != code {
		t.Fatalf("unsafe error: %s", err)
	}
	return got
}

// TestNumberValidationBeforeIO checks that invalid international numbers fail before any provider I/O.
func TestNumberValidationBeforeIO(t *testing.T) {
	cases := []struct {
		name, input string
		valid       bool
	}{
		{"local", "07700900123", false}, {"missing_plus", "12025550123", false},
		{"invalid_country", "+999123456789", false}, {"invalid_national", "+4407700900123", false},
		{"noncanonical_spaces", "+44 7700 900123", false}, {"too_short", "+1", false},
		{"extension", "+12025550123 ext2", false}, {"US", phone, true}, {"France", "+33612345678", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.valid {
				if err := ValidateNumber(tc.input); err != nil {
					t.Fatal(err)
				}
				return
			}
			var calls atomic.Int32
			svc := configured(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = fmt.Fprint(w, registered) }, nil)
			result, err := svc.CheckNumber(context.Background(), tc.input)
			requireCode(t, err, CodeInvalidNumber)
			if result.Status != "unknown" {
				t.Fatal(result)
			}
			_, err = svc.SendDirect(context.Background(), tc.input, "hello")
			requireCode(t, err, CodeInvalidNumber)
			_, err = svc.CreateGroup(context.Background(), "test", []string{phone, tc.input})
			requireCode(t, err, CodeInvalidNumber)
			if calls.Load() != 0 {
				t.Fatalf("invalid input made %d calls", calls.Load())
			}
		})
	}
}

// TestRegistrationContract distinguishes authoritative evidence from malformed or mismatched provider replies.
func TestRegistrationContract(t *testing.T) {
	cases := []struct{ name, body, status, code string }{
		{"registered", registered, "registered", ""},
		{"not registered", absent, "not_registered", CodeNotRegistered},
		{"opaque identity", `{"number":"12025550123","exists":true,"whatsappId":"987654321@lid"}`, "registered", ""},
		{"missing exists", `{"number":"12025550123","whatsappId":null}`, "unknown", CodeInvalidResponse},
		{"wrong number", `{"number":"12025550124","exists":false,"whatsappId":null}`, "unknown", CodeInvalidResponse},
		{"missing id", `{"number":"12025550123","exists":false}`, "unknown", CodeInvalidResponse},
		{"negative with id", `{"number":"12025550123","exists":false,"whatsappId":"12025550123@c.us"}`, "unknown", CodeInvalidResponse},
		{"positive with group", `{"number":"12025550123","exists":true,"whatsappId":"123@g.us"}`, "unknown", CodeInvalidResponse},
		{"positive with null", `{"number":"12025550123","exists":true,"whatsappId":null}`, "unknown", CodeInvalidResponse},
		{"wrong boolean", `{"number":"12025550123","exists":"false","whatsappId":null}`, "unknown", CodeInvalidResponse},
		{"html", `<html>error</html>`, "unknown", CodeInvalidResponse},
		{"trailing document", registered + ` {}`, "unknown", CodeInvalidResponse},
		{"oversized", strings.Repeat(" ", int(maxResponseBytes)) + registered, "unknown", CodeInvalidResponse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := configured(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/sessions/"+session+"/contacts/check/12025550123" {
					t.Errorf("wrong request %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("X-API-Key") != "test-only-key" {
					t.Error("missing key")
				}
				_, _ = fmt.Fprint(w, tc.body)
			}, nil)
			result, err := s.CheckNumber(context.Background(), phone)
			if tc.status == "unknown" {
				_ = requireCode(t, err, tc.code)
			} else if err != nil {
				t.Fatal(err)
			}
			if result.Status != tc.status || result.Code != tc.code || result.Number != phone || result.CheckedAt.IsZero() {
				t.Fatalf("result = %+v", result)
			}
			data, _ := json.Marshal(result)
			if strings.Contains(string(data), "@") {
				t.Fatal("canonical ID leaked to browser")
			}
		})
	}
}

// TestReadFailuresRemainUnknown checks safe error mapping and prevents read failures becoming negative evidence.
func TestReadFailuresRemainUnknown(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{400, CodeUnavailable}, {401, CodeAuthFailed}, {403, CodePermissionDenied}, {404, CodeUnavailable}, {409, CodeSessionNotReady}, {429, CodeRateLimited}, {503, CodeUnavailable}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			s := configured(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, "sensitive provider diagnostic")
			}, nil)
			result, err := s.CheckNumber(context.Background(), phone)
			e := requireCode(t, err, tc.code)
			if result.Status != "unknown" || e.RetryAfter != 7*time.Second {
				t.Fatal(result, e)
			}
		})
	}
}

// TestCancellationTimeoutAndRedirectIsolation checks cancellation, deadlines and credential-bearing redirect refusal.
func TestCancellationTimeoutAndRedirectIsolation(t *testing.T) {
	for _, tc := range []struct {
		name                string
		redirect, cancelled bool
	}{
		{"redirect", true, false}, {"deadline", false, false}, {"cancelled", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
			t.Cleanup(target.Close)
			svc := configured(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.redirect {
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
					return
				}
				<-r.Context().Done()
			}, func(c *Config) { c.Timeout = 10 * time.Millisecond })
			ctx := context.Background()
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := svc.CheckNumber(ctx, phone)
			if tc.cancelled {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			requireCode(t, err, CodeUnavailable)
			if result.Status != "unknown" {
				t.Fatal(result)
			}
			if tc.redirect {
				_, err = svc.SendGroup(ctx, group, "hello")
				requireCode(t, err, CodeUncertain)
				if redirected.Load() != 0 {
					t.Fatal("followed credential-bearing redirect")
				}
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	}
}

// TestReadRetryBudgetAndMutationNoReplay checks bounded reads, retry guidance and single-attempt mutations.
func TestReadRetryBudgetAndMutationNoReplay(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		succeeds bool
	}{
		{"retry_to_success", 503, true}, {"authentication", 401, false}, {"rate_guidance", 429, false}, {"unavailable_guidance", 503, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			svc := configured(t, func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if tc.succeeds && n >= 3 {
					_, _ = fmt.Fprint(w, registered)
					return
				}
				if !tc.succeeds {
					w.Header().Set("Retry-After", "120")
				}
				w.WriteHeader(tc.status)
			}, func(c *Config) { c.ReadRetryLimit = 2 })
			result, err := svc.CheckNumber(context.Background(), phone)
			if tc.succeeds {
				if err != nil || result.Status != "registered" || calls.Load() != 3 {
					t.Fatal(result, err, calls.Load())
				}
				return
			}
			if err == nil || calls.Load() != 1 {
				t.Fatal("retried authentication failure or before Retry-After", err, calls.Load())
			}
			calls.Store(0)
			_, err = svc.SendGroup(context.Background(), group, "hello")
			if err == nil || calls.Load() != 1 {
				t.Fatal("mutation replay", err, calls.Load())
			}
		})
	}
}

// TestMessageAndGroupOperations exercises resolved direct/group text, quoting, creation and invite-join contracts.
func TestMessageAndGroupOperations(t *testing.T) {
	cases := []struct {
		name, operation, suffix string
		calls                   int
	}{
		{"direct", "direct", "/send-text", 2}, {"group", "group", "/send-text", 1},
		{"reply", "reply", "/reply", 1}, {"create", "create", "/groups", 2}, {"join", "join", "/groups/join", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			svc := configured(t, func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.Method == "GET" {
					_, _ = fmt.Fprint(w, registered)
					return
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				switch tc.operation {
				case "direct", "group", "reply":
					if body["text"] != "hello" || (body["chatId"] != jid && body["chatId"] != group) {
						t.Errorf("wrong body %+v", body)
					}
					if tc.operation == "reply" && body["quotedMessageId"] != "quoted_opaque" {
						t.Error(body)
					}
					w.WriteHeader(201)
					_, _ = fmt.Fprint(w, `{"messageId":"opaque_message","timestamp":1800000000}`)
				case "join":
					if body["inviteCode"] != "test_invite" {
						t.Error(body)
					}
					_, _ = fmt.Fprintf(w, `{"success":true,"groupId":%q}`, group)
				case "create":
					ids, ok := body["participants"].([]any)
					if !ok || len(ids) != 1 || ids[0] != jid {
						t.Error(body)
					}
					w.WriteHeader(201)
					_, _ = fmt.Fprintf(w, `{"id":%q,"name":"test"}`, group)
				}
			}, func(c *Config) { c.OpenWA.Engine = "baileys" })
			var receipt MessageReceipt
			var got GroupReceipt
			var err error
			switch tc.operation {
			case "direct":
				receipt, err = svc.SendDirect(context.Background(), phone, "hello")
			case "group":
				receipt, err = svc.SendGroup(context.Background(), group, "hello")
			case "reply":
				receipt, err = svc.ReplyToMessage(context.Background(), jid, "quoted_opaque", "hello")
			case "create":
				got, err = svc.CreateGroup(context.Background(), "test", []string{phone, jid})
			case "join":
				got, err = svc.JoinGroup(context.Background(), "test_invite")
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.operation == "create" || tc.operation == "join" {
				if got.GroupID != group {
					t.Fatal(got)
				}
			} else if receipt.State != "accepted" {
				t.Fatal(receipt)
			}
			if len(paths) != tc.calls || !strings.HasSuffix(paths[len(paths)-1], tc.suffix) {
				t.Fatal("wrong lookup/mutation path sequence", paths)
			}
			if !svc.Capabilities().CreateGroup {
				t.Fatal("missing Baileys capability")
			}
		})
	}
}

// TestUnregisteredAndInvalidDestinationsNeverSend checks that invalid or unregistered destinations cause no mutations.
func TestUnregisteredAndInvalidDestinationsNeverSend(t *testing.T) {
	for _, tc := range []struct{ name, operation, code string }{
		{"unregistered_direct", "direct", CodeNotRegistered}, {"unregistered_create", "create", CodeNotRegistered},
		{"contact_is_not_group", "group", CodeInvalidRequest}, {"invite_url", "join", CodeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mutations atomic.Int32
			svc := configured(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					mutations.Add(1)
				}
				_, _ = fmt.Fprint(w, absent)
			}, nil)
			var err error
			switch tc.operation {
			case "direct":
				_, err = svc.SendDirect(context.Background(), phone, "hello")
			case "create":
				_, err = svc.CreateGroup(context.Background(), "test", []string{phone})
			case "group":
				_, err = svc.SendGroup(context.Background(), jid, "hello")
			case "join":
				_, err = svc.JoinGroup(context.Background(), "https://evil.example/invite")
			}
			requireCode(t, err, tc.code)
			if mutations.Load() != 0 {
				t.Fatal(mutations.Load())
			}
		})
	}
}

// TestMutationOutcomes distinguishes definite rejections from uncertain outcomes and known engine limitations.
func TestMutationOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name              string
		status            int
		body, code        string
		uncertain         bool
		unsupportedEngine bool
	}{
		{"invalid invite", 400, `{}`, CodeInvalidInvite, false, false}, {"denied", 403, `{}`, CodePermissionDenied, false, false},
		{"unsupported", 501, `{}`, CodeUnsupported, false, false}, {"timeout upstream", 503, `{}`, CodeUncertain, true, false},
		{"malformed acceptance", 200, `{}`, CodeUncertain, true, false}, {"unsuccessful", 200, `{"success":false}`, CodeUncertain, true, false},
		{"wrong group", 200, `{"success":true,"groupId":"1@c.us"}`, CodeUncertain, true, false},
		{"unsupported engine", 503, "", CodeUnsupported, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			s := configured(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}, func(c *Config) {
				if tc.unsupportedEngine {
					c.OpenWA.Engine = "whatsapp-web.js"
				}
			})
			var err error
			wantCalls := 1
			if tc.unsupportedEngine {
				_, err = s.CreateGroup(context.Background(), "test", []string{phone})
				wantCalls = 0
			} else {
				_, err = s.JoinGroup(context.Background(), "test_invite")
			}
			e := requireCode(t, err, tc.code)
			if e.Uncertain != tc.uncertain || calls != wantCalls {
				t.Fatal(e, calls)
			}
		})
	}
}

// This ordered traversal is intentionally standalone: the outgoing-only first
// page supplies the exact cursor used by the next read, followed by a head rescan.
func TestReplyPagesPreserveIdentityAndRawCursor(t *testing.T) {
	var queries []string
	s := configured(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("chatId") != group || r.URL.Query().Get("inlineMedia") != "false" || r.URL.Query().Get("limit") != "2" {
			t.Error(r.URL)
		}
		after := r.URL.Query().Get("after")
		rows := []map[string]any{}
		makeRow := func(id, direction string) map[string]any {
			return map[string]any{"id": id, "waMessageId": "wa_" + id, "sessionId": session, "chatId": group, "from": group, "author": "123456789@lid", "direction": direction, "body": "hello", "type": "text", "status": "received", "timestamp": 1800000000, "metadata": map[string]any{"quotedMessage": map[string]string{"id": "quoted"}}}
		}
		switch after {
		case "":
			rows = append(rows, makeRow("row2", "outgoing"), makeRow("row1", "outgoing"))
		case "row1":
			rows = append(rows, makeRow("row0", "incoming"))
		default:
			t.Error(after)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"messages": rows, "total": 3})
	}, nil)
	first, err := s.ListReplies(context.Background(), MessageQuery{ChatID: group, Limit: 2})
	if err != nil || len(first.Messages) != 0 || first.NextCursor != "row1" || !first.HasMore {
		t.Fatal(first, err)
	}
	next, err := s.ListReplies(context.Background(), MessageQuery{ChatID: group, Limit: 2, After: first.NextCursor})
	if err != nil || next.HasMore || len(next.Messages) != 1 {
		t.Fatal(next, err)
	}
	m := next.Messages[0]
	if m.RowID == m.MessageID || m.Author != "123456789@lid" || m.Sender != group || m.QuotedMessageID != "quoted" {
		t.Fatal(m)
	}
	_, _ = s.ListReplies(context.Background(), MessageQuery{ChatID: group, Limit: 2})
	if queries[0] != queries[2] {
		t.Fatal("new poll did not rescan newest")
	}
}

// TestReplyContractRejectsCrossScopeAndBadPages rejects missing row data and incorrect session/chat/direction evidence.
func TestReplyContractRejectsCrossScopeAndBadPages(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"missing messages", `{"total":0}`}, {"null messages", `{"messages":null,"total":0}`},
		{"wrong session", fmt.Sprintf(`{"messages":[{"id":"row","sessionId":"other","chatId":%q,"timestamp":1}],"total":1}`, group)},
		{"wrong chat", fmt.Sprintf(`{"messages":[{"id":"row","sessionId":%q,"chatId":"999@g.us","timestamp":1,"direction":"incoming"}],"total":1}`, session)},
		{"bad direction", fmt.Sprintf(`{"messages":[{"id":"row","sessionId":%q,"chatId":%q,"timestamp":1,"direction":"other"}],"total":1}`, session, group)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := configured(t, func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, tc.body) }, nil)
			_, err := s.ListReplies(context.Background(), MessageQuery{ChatID: group})
			_ = requireCode(t, err, CodeInvalidResponse)
		})
	}
}

// fakeProvider isolates the injected-provider seam from OpenWA transport behaviour.
type fakeProvider struct {
	// called counts registration requests to verify delegation and validation order.
	called int
}

// Capabilities advertises only the registration behaviour used by this injected test provider.
func (f *fakeProvider) Capabilities() Capabilities { return Capabilities{CheckNumber: true} }

// CheckNumber records a call and returns matching registration evidence with an opaque contact identity.
func (f *fakeProvider) CheckNumber(_ context.Context, n string) (Registration, error) {
	f.called++
	return Registration{Status: "registered", Number: n, CanonicalID: "987@lid", CheckedAt: time.Now()}, nil
}

// SendText rejects messaging because this fixture supplies registration only.
func (f *fakeProvider) SendText(context.Context, MessageRequest) (MessageReceipt, error) {
	return MessageReceipt{}, invalid(CodeUnsupported)
}

// CreateGroup rejects creation because this fixture supplies registration only.
func (f *fakeProvider) CreateGroup(context.Context, GroupRequest) (GroupReceipt, error) {
	return GroupReceipt{}, invalid(CodeUnsupported)
}

// JoinGroup rejects invite acceptance because this fixture supplies registration only.
func (f *fakeProvider) JoinGroup(context.Context, string) (GroupReceipt, error) {
	return GroupReceipt{}, invalid(CodeUnsupported)
}

// ListMessages rejects reply reads because this fixture supplies registration only.
func (f *fakeProvider) ListMessages(context.Context, MessageQuery) (MessagePage, error) {
	return MessagePage{}, invalid(CodeUnsupported)
}

// TestConstructorAndProviderSeam checks disabled/injected providers and rejects unsafe or conflicting configuration.
func TestConstructorAndProviderSeam(t *testing.T) {
	for _, tc := range []struct {
		name, provider string
		injected       bool
	}{
		{"injected", "future", true}, {"default_disabled", "", false}, {"explicit_disabled", "disabled", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeProvider{}
			cfg := Config{Provider: tc.provider}
			if tc.injected {
				cfg.Adapter = f
			}
			svc, err := NewTeleProvider(cfg)
			if err != nil {
				t.Fatal(err)
			}
			r, err := svc.CheckNumber(context.Background(), phone)
			if tc.injected {
				if err != nil || r.CanonicalID != "987@lid" || f.called != 1 {
					t.Fatal(r, err)
				}
			} else {
				requireCode(t, err, CodeUnavailable)
				if r.Status != "unknown" || f.called != 0 {
					t.Fatal(r, f.called)
				}
			}
		})
	}
	var typedNil *fakeProvider
	for _, tc := range []struct {
		name   string
		cfg    Config
		inject bool
	}{
		{"unknown", Config{Provider: "unknown"}, false}, {"incomplete_openwa", Config{Provider: "openwa"}, false},
		{"typed_nil", Config{Provider: "future", Adapter: typedNil}, false},
		{"disabled_adapter", Config{Provider: "disabled"}, true},
		{"conflicting_openwa", Config{Provider: "future", OpenWA: OpenWAConfig{APIKey: "fixture-only"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			if tc.inject {
				cfg.Adapter = &fakeProvider{}
			}
			if _, err := NewTeleProvider(cfg); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
	for _, tc := range []struct{ name, endpoint string }{
		{"remote_http", "http://remote.example/api"}, {"userinfo", "https://user:pass@example.com/api"},
		{"query", "https://example.com/api?secret=x"}, {"fragment", "https://example.com/api#x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTeleProvider(Config{Provider: "openwa", OpenWA: OpenWAConfig{Endpoint: tc.endpoint, APIKey: "test", SessionID: session}})
			requireCode(t, err, CodeInvalidRequest)
		})
	}
}

// TestMalformedMutationReceiptsAreUncertain prevents incomplete success replies from justifying mutation replay.
func TestMalformedMutationReceiptsAreUncertain(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		create     bool
	}{
		{"send missing timestamp", `{"messageId":"known"}`, false},
		{"send missing identity", `{"timestamp":1800000000}`, false},
		{"create wrong identity", `{"id":"123@c.us","name":"test"}`, true},
		{"create missing name", `{"id":"120363000000000000@g.us"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := configured(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(201)
				_, _ = fmt.Fprint(w, tc.body)
			}, nil)
			var err error
			if tc.create {
				_, err = s.CreateGroup(context.Background(), "test", []string{jid})
			} else {
				_, err = s.SendGroup(context.Background(), group, "hello")
			}
			e := requireCode(t, err, CodeUncertain)
			if !e.Uncertain || calls != 1 {
				t.Fatal(e, calls)
			}
		})
	}
}

// TestExpiredReplyCursorReportsGap keeps a rejected older-page cursor visible to the consuming service.
func TestExpiredReplyCursorReportsGap(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		code   string
	}{{"bad_request", 400, CodeCursorUnavailable}, {"missing_endpoint", 404, CodeUnavailable}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := configured(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) }, nil)
			_, err := svc.ListReplies(context.Background(), MessageQuery{ChatID: group, After: "expired-row"})
			requireCode(t, err, tc.code)
		})
	}
}
