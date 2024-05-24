package common

const (

	// RFC3339NanoUTC is the time format we use for all
	// our date time. It will be used for parsing our UTC date time.
	RFC3339NanoUTC string = "2006-01-02T15:04:05.999999999"
)

// System wide variables
const (

	// SystemWideXApiToken is the token idetifier we expect when user wants to use their
	// apitoken
	SystemWideXApiToken string = "X-Api-Token"

	// HtmxHttpRequestHeader is the request header that is passed with all htmx request,
	// the value is set to "true" when a request is made with the library
	HtmxHttpRequestHeader string = "Hx-Request"

	// HtmxHttpCurrentUrlHeader is the request header that is passed with htmx requests
	HtmxHttpCurrentUrlHeader string = "Hx-Current-Url"

	// HtmxHttpCurrentUrlHeader is the request header that is passed with htmx requests
	HtmxHttpTargetHeader string = "Hx-Target"

	// HtmxHttpTriggerHeader is the request header that is passed with htmx requests
	HtmxHttpTriggerHeader string = "Hx-Trigger"

	// CorrelationIdHttpHeader is the header used to identify the request's id
	CorrelationIdHttpHeader string = "X-Correlation-Id"

	// WebPartialHttpRequestHeader is the header used to tell server that client only requires
	// a partial response from the endpoint
	WebPartialHttpRequestHeader string = "X-Web-Partial"

	// WebLocationHttpRequestHeader is the header used to inform the client where user
	// should be redirect to
	WebLocationHttpRequestHeader string = "X-Web-Location"

	// CacheSkipHttpResponseHeader is the response header used to tell server not to cache the
	// response from the endpoint
	CacheSkipHttpResponseHeader string = "X-Cache-Skip"

	// ClouflareForwardingIPAddressHttpHeader represents the header used by cloudflare to identify
	// the IP address the rquest is being proxied on behalf of.
	ClouflareForwardingIPAddressHttpHeader = "Cf-Connecting-Ip"

	// ApiV1UriPrefix the prefix that will be added to all of the Api's V1 URI routes
	ApiV1UriPrefix = "/api/v1"

	// WebNextStepsHttpQueryParam is the query parameter used to tell the api where to redirect the user to
	WebNextStepsHttpQueryParam = "next_step"

	AccessTokenAuthInfoCookieName  string = "access_token"
	RefreshTokenAuthInfoCookieName string = "refresh_token"
)

const (
	// FairUsageHighTier is the maximum number of fair usage
	FairUsageHighTier int64 = 5000

	// FairUsageMediumTier is the maximum number of fair usage
	FairUsageMediumTier int64 = 2750

	// DefaultRoleRanking is the default ranking given to the default role
	DefaultRoleRanking int64 = 888
)

// UserRole represents the user roles on the platform
type UserRole string

const (
	// UserRoleAdmin that represents "Admin" user
	UserRoleAdmin UserRole = "ADMIN"

	// UserRoleHighTeir that represents the high tier user role
	UserRoleHighTeir UserRole = "MAX"

	// UserRoleMidTeir that represents the mid tier user role
	UserRoleMidTeir UserRole = "PRO"

	// UserRoleDefaultTeir that represents the default tier user role
	UserRoleDefaultTeir UserRole = "DEFAULT"
)

var (
	// UserRolesThresholds is the system wide thresholds applied to specific roles
	// roles are listed in their level of importance where the first in the list is the
	// highest rank role (excluding ADMIN)
	UserRolesThresholds = struct {
		RolesDetails map[UserRole]UserRoleThresholds
	}{
		RolesDetails: map[UserRole]UserRoleThresholds{
			UserRoleAdmin: {
				Ranking:                      0,
				LongLivedUserTokenLimit:      9999,
				ShortLivedUserTokenLimit:     9999,
				ShortLivedMinimumAllowedTime: 60,      // 1 minute
				ShortLivedMaximumAllowedTime: 2628337, // 1 month
				ShortLivedMinimumIncrements:  1,
				//////
			},
			UserRoleHighTeir: {
				Ranking:                      10,
				LongLivedUserTokenLimit:      3,
				ShortLivedUserTokenLimit:     3,
				ShortLivedMinimumAllowedTime: 1500,  //25 minutes as seconds
				ShortLivedMaximumAllowedTime: 21600, // 6 hours
				ShortLivedMinimumIncrements:  300,   // 5 min
				//////
			},
			UserRoleMidTeir: {
				Ranking:                      11,
				LongLivedUserTokenLimit:      2,
				ShortLivedUserTokenLimit:     1,
				ShortLivedMinimumAllowedTime: 3600,  //60 minutes as seconds
				ShortLivedMaximumAllowedTime: 10800, // 3 hours
				ShortLivedMinimumIncrements:  600,   // 10min
				//////
			},
			UserRoleDefaultTeir: {
				Ranking:                      DefaultRoleRanking,
				LongLivedUserTokenLimit:      1,
				ShortLivedUserTokenLimit:     0,
				ShortLivedMinimumAllowedTime: 0,
				ShortLivedMaximumAllowedTime: 0,
				ShortLivedMinimumIncrements:  0,
				//////
			},
		},
	}
)

// UserRoleThresholds holds the different thresholds types associated to
// a particular user role
type UserRoleThresholds struct {
	// Ranking is how "powerful" a role is on the platform
	// where the lower the number the more
	// threshold is allocated, where 0 is admin
	Ranking int64

	// LongLivedUserTokenLimit is the number of permanent
	// auth tokens a user is permitted
	LongLivedUserTokenLimit int64
	// ShortLivedUserTokenLimit is the number of ephemeral
	// auth tokens a user is permitted
	ShortLivedUserTokenLimit int64
	// ShortLivedMinimumAllowedTime is the minimum time (in seconds) of which
	// a user's ephemeral auth tokens is permitted to last
	ShortLivedMinimumAllowedTime int64
	// ShortLivedMaximumAllowedTime is the maximum time (in seconds) of which
	// a user's ephemeral auth tokens is permitted to last
	ShortLivedMaximumAllowedTime int64
	// ShortLivedMinimumIncrements is the minimum time jumps [incremental steps] (in seconds) a
	// a user's ephemeral auth tokens can have
	ShortLivedMinimumIncrements int64
}

// GetUsersHighestRankedRole is looking at the passed roles assigned to a
// user and returning their highest ranked role to see what thresholds
// are available to them
//
// the lower the number, the higher their rank. If not role detected, user should
// be given default role
func GetUsersHighestRankedRole(assignedUserRoles []string) string {

	var defaultRolePlaceholder int64 = DefaultRoleRanking

	// loop through assigned role and see  if there are any matches
	for _, assignedRole := range assignedUserRoles {

		// TODO: Should we just return ADMIN one time if detected, no point
		// wasting resources with the rest computation
		if UserRole(assignedRole) == UserRoleAdmin {
			return assignedRole
		}

		for thresholdHolderRole, roleDetails := range UserRolesThresholds.RolesDetails {

			if UserRole(assignedRole) == thresholdHolderRole {

				if roleDetails.Ranking < defaultRolePlaceholder {
					defaultRolePlaceholder = roleDetails.Ranking
				}
			}
			continue
		}
	}

	if defaultRolePlaceholder == DefaultRoleRanking {
		return string(UserRoleDefaultTeir)
	}

	// loop through thresholds
	for thresholdHolderRole, roleDetails := range UserRolesThresholds.RolesDetails {

		if roleDetails.Ranking == defaultRolePlaceholder {

			return string(thresholdHolderRole)
		}
	}

	return string(UserRoleDefaultTeir)
}
