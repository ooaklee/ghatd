package usermanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/stretchr/testify/require"
)

// mappedIdentity captures the authority and mutable payload delivered to a
// manager service, independently of each endpoint's lower-domain target fields.
type mappedIdentity struct {
	// actor is the authenticated caller, never a body-provided identity.
	actor string
	// target and member identify URL-selected resources when the route has them.
	target, member string
	// value is a legitimate body field which must survive identity binding.
	value string
	// wholeGroup reports whether HTTP exposed the internal full-record update.
	wholeGroup bool
}

// mapIdentityFixture calls the real mappers and projects their request contracts
// into comparable observations. No service or persistence is invoked here.
func mapIdentityFixture(operation string, r *http.Request) (mappedIdentity, error) {
	v := usermanagerAuthTestValidator{}
	switch operation {
	case "delete self":
		p, err := MapRequestToDeleteUserPermanentlyRequest(r, v)
		if err != nil {
			return mappedIdentity{}, err
		}
		return mappedIdentity{actor: p.ActorID, target: p.ID, value: p.Reason}, nil
	case "create contact":
		p, err := MapRequestToCreateCommsRequest(r, v)
		if err != nil {
			return mappedIdentity{}, err
		}
		return mappedIdentity{actor: p.UserId, value: p.Message}, nil
	case "update contact":
		p, err := MapRequestToUpdateCommsRequest(r, v)
		if err != nil {
			return mappedIdentity{}, err
		}
		value := ""
		if p.AdminNotes != nil {
			value = *p.AdminNotes
		}
		return mappedIdentity{actor: p.ActorID, target: p.CommsId, value: value}, nil
	case "create group":
		p, err := MapRequestToCreateGroupRequest(r, v)
		if err != nil {
			return mappedIdentity{}, err
		}
		return mappedIdentity{actor: p.ActorID, target: p.ParentGroupID, member: p.OwnerID, value: p.Name}, nil
	case "update group":
		p, err := MapRequestToUpdateGroupRequest(r, v)
		if err != nil {
			return mappedIdentity{}, err
		}
		value := ""
		if p.Name != nil {
			value = *p.Name
		}
		return mappedIdentity{actor: p.ActorID, target: p.ID, value: value, wholeGroup: p.Group != nil}, nil
	case "add member":
		p, err := MapRequestToAddGroupMemberRequest(r, v)
		if err != nil {
			return mappedIdentity{}, err
		}
		return mappedIdentity{actor: p.ActorID, target: p.GroupID, member: p.MemberID, value: p.Role}, nil
	case "update member":
		p, err := MapRequestToUpdateGroupMemberRequest(r, v)
		if err != nil {
			return mappedIdentity{}, err
		}
		return mappedIdentity{actor: p.ActorID, target: p.GroupID, member: p.MemberID, value: p.NewRole}, nil
	case "update owner":
		p, err := MapRequestToUpdateGroupOwnerRequest(r, v)
		if err != nil {
			return mappedIdentity{}, err
		}
		owner := ""
		if p.OwnerID != nil {
			owner = *p.OwnerID
		}
		return mappedIdentity{actor: p.ActorID, target: p.GroupID, member: owner}, nil
	default:
		panic("unknown identity fixture operation")
	}
}

// TestMutationMappersBindIdentityAfterDecode covers attacker-controlled JSON,
// case-insensitive keys, null fields, query values and anonymous context IDs.
func TestMutationMappersBindIdentityAfterDecode(t *testing.T) {
	for _, endpoint := range []struct {
		name string
		body string
		want mappedIdentity
	}{
		{"delete self", `"reason":"Leaving"`, mappedIdentity{actor: "caller", target: "caller", value: "Leaving"}},
		{"create contact", `"message":"Hello"`, mappedIdentity{actor: "caller", value: "Hello"}},
		{"update contact", `"admin_notes":"Reviewed"`, mappedIdentity{actor: "caller", target: "route-contact", value: "Reviewed"}},
		{"create group", `"name":"Team","parent_group_id":"parent","owner_id":"chosen-owner"`, mappedIdentity{actor: "caller", target: "parent", member: "chosen-owner", value: "Team"}},
		{"update group", `"name":"Renamed"`, mappedIdentity{actor: "caller", target: "route-group", value: "Renamed"}},
		{"add member", `"member_id":"chosen-member","role":"MEMBER"`, mappedIdentity{actor: "caller", target: "route-group", member: "chosen-member", value: "MEMBER"}},
		{"update member", `"new_role":"MEMBER"`, mappedIdentity{actor: "caller", target: "route-group", member: "route-member", value: "MEMBER"}},
		{"update owner", `"owner_id":"chosen-owner"`, mappedIdentity{actor: "caller", target: "route-group", member: "chosen-owner"}},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, payload := range []struct{ name, suffix string }{
				{"ordinary", ""},
				{"forged actor", `,"ActorID":"forged","actorid":"forged","actor_id":"forged"`},
				{"null actor", `,"ActorID":null,"actorid":null,"actor_id":null`},
				{"forged IDs", `,"UserId":"forged","UserID":"forged","ID":"forged","GroupID":"forged","MemberID":"forged","CommsId":"forged"`},
				{"case variants", `,"userid":"forged","id":"forged","groupid":"forged","memberid":"forged","commsid":"forged"`},
				{"null IDs", `,"UserId":null,"ID":null,"GroupID":null,"MemberID":null,"CommsId":null`},
				{"nested group target", `,"group":{"id":"forged","owner_id":"forged"}`},
				{"null embedded payload", `,"CreateGroupRequest":null,"AddMemberRequest":null,"UpdateMemberRoleRequest":null,"UpdateOwnerRequest":null`},
			} {
				t.Run(payload.name, func(t *testing.T) {
					for _, identity := range []struct {
						name, id      string
						authenticated bool
					}{
						{"verified", "caller", true},
						{"anonymous placeholder", "caller", false},
						{"missing ID", "", true},
					} {
						t.Run(identity.name, func(t *testing.T) {
							ctx := accessmanagerhelpers.TransitWith(context.Background(), identity.id)
							ctx = accessmanagerhelpers.TransitAuthenticatedWith(ctx, identity.authenticated)
							r := httptest.NewRequest(http.MethodPost, "/?UserId=forged&user_id=forged&GroupID=forged&id=forged", strings.NewReader("{"+endpoint.body+payload.suffix+"}")).WithContext(ctx)
							r = mux.SetURLVars(r, map[string]string{"id": "route-contact", "groupID": "route-group", "memberID": "route-member"})
							got, err := mapIdentityFixture(endpoint.name, r)
							if (!identity.authenticated || identity.id == "") && endpoint.name != "create contact" {
								require.ErrorIs(t, err, ErrUnableToIdentifyUser)
								return
							}
							require.NoError(t, err)
							want := endpoint.want
							if !identity.authenticated || identity.id == "" {
								want.actor = ""
							}
							require.Equal(t, want, got)
						})
					}
				})
			}
		})
	}
}

// TestMutationMappersRejectInvalidTransport covers decode and route failures
// before any manager service is called, with the existing error vocabulary.
func TestMutationMappersRejectInvalidTransport(t *testing.T) {
	for _, operation := range []string{"delete self", "create contact", "update contact", "create group", "update group", "add member", "update member", "update owner"} {
		t.Run(operation, func(t *testing.T) {
			for _, body := range []string{"", "{", "[]"} {
				t.Run("body="+body, func(t *testing.T) {
					r := identityHTTPRequest("caller", true, body)
					r = mux.SetURLVars(r, map[string]string{"id": "contact", "groupID": "group", "memberID": "member"})
					_, err := mapIdentityFixture(operation, r)
					want := ErrRequestFailedValidation
					if operation == "create contact" || operation == "update contact" {
						want = contacter.ErrInvalidCommsPayload
					}
					require.ErrorIs(t, err, want)
				})
			}
		})
	}
	for _, tc := range []struct {
		operation, missing string
		want               error
	}{
		{"update contact", "id", ErrRequestFailedValidation},
		{"update group", "groupID", ErrRequestFailedValidation},
		{"add member", "groupID", ErrRequestFailedValidation},
		{"update member", "groupID", ErrRequestFailedValidation},
		{"update member", "memberID", ErrInvalidMemberID},
		{"update owner", "groupID", ErrRequestFailedValidation},
	} {
		t.Run(tc.operation+" missing "+tc.missing, func(t *testing.T) {
			vars := map[string]string{"id": "contact", "groupID": "group", "memberID": "member"}
			delete(vars, tc.missing)
			r := mux.SetURLVars(identityHTTPRequest("caller", true, `{}`), vars)
			_, err := mapIdentityFixture(tc.operation, r)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

// TestGroupMutationPayloadCompatibility keeps pointer/clear semantics and all
// supported editable group fields while excluding internal replacement records.
func TestGroupMutationPayloadCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       *string
	}{
		{"omitted owner", `{}`, nil},
		{"null owner", `{"owner_id":null}`, nil},
		{"clear owner", `{"owner_id":""}`, new(string)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := MapRequestToUpdateGroupOwnerRequest(identityHTTPRequest("caller", true, tc.body), usermanagerAuthTestValidator{})
			require.NoError(t, err)
			require.Equal(t, tc.want, p.OwnerID)
		})
	}
	for _, tc := range []struct {
		name, body string
		provided   bool
	}{
		{"omitted fields", `{}`, false},
		{"null fields", `{"name":null,"description":null,"email":null,"icon":null,"visibility":null,"status":null,"extensions":null}`, false},
		{"editable fields", `{"name":"n","description":"d","email":"e","icon":"i","visibility":"v","status":"s","extensions":{"custom":true}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := MapRequestToUpdateGroupRequest(identityHTTPRequest("caller", true, tc.body), usermanagerAuthTestValidator{})
			require.NoError(t, err)
			require.Equal(t, "route-group", p.ID)
			require.Nil(t, p.Group)
			for _, field := range []struct {
				value *string
				want  string
			}{
				{p.Name, "n"}, {p.Description, "d"}, {p.Email, "e"}, {p.Icon, "i"}, {p.Visibility, "v"}, {p.Status, "s"},
			} {
				if tc.provided {
					require.NotNil(t, field.value)
					require.Equal(t, field.want, *field.value)
				} else {
					require.Nil(t, field.value)
				}
			}
			if tc.provided {
				require.Equal(t, map[string]interface{}{"custom": true}, p.Extensions)
			} else {
				require.Nil(t, p.Extensions)
			}
		})
	}
}
