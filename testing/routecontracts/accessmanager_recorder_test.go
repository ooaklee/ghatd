package routecontracts_test

import "net/http"

// Access Manager ports share the domain dispatch recorder. Each forwarding
// method deliberately identifies the handler rather than trusting metadata.
func (h *routeRecorder) CreateInitalLoginOrVerificationTokenEmail(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "CreateInitalLoginOrVerificationTokenEmail")
}
func (h *routeRecorder) LogoutUser(w http.ResponseWriter, _ *http.Request) { h.record(w, "LogoutUser") }
func (h *routeRecorder) RefreshToken(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "RefreshToken")
}
func (h *routeRecorder) OauthCallback(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "OauthCallback")
}
func (h *routeRecorder) OauthLogin(w http.ResponseWriter, _ *http.Request) { h.record(w, "OauthLogin") }
func (h *routeRecorder) MobileOAuthProviders(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "MobileOAuthProviders")
}
func (h *routeRecorder) MobileOAuthLogin(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "MobileOAuthLogin")
}
func (h *routeRecorder) MobileOAuthLink(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "MobileOAuthLink")
}
func (h *routeRecorder) MobileOAuthStart(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "MobileOAuthStart")
}
func (h *routeRecorder) MobileOAuthExchange(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "MobileOAuthExchange")
}
func (h *routeRecorder) OAuthProviders(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "OAuthProviders")
}
func (h *routeRecorder) OAuthLink(w http.ResponseWriter, _ *http.Request) { h.record(w, "OAuthLink") }
func (h *routeRecorder) OAuthConnections(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "OAuthConnections")
}
func (h *routeRecorder) StartOAuthDisconnect(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "StartOAuthDisconnect")
}
func (h *routeRecorder) ConfirmOAuthDisconnect(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "ConfirmOAuthDisconnect")
}
func (h *routeRecorder) ReviewOAuthDisconnectChallenge(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "ReviewOAuthDisconnectChallenge")
}
func (h *routeRecorder) OAuthConnectionVerification(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "OAuthConnectionVerification")
}
func (h *routeRecorder) MobileOAuthConnections(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "MobileOAuthConnections")
}
func (h *routeRecorder) StartMobileOAuthDisconnect(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "StartMobileOAuthDisconnect")
}
func (h *routeRecorder) ConfirmMobileOAuthDisconnect(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "ConfirmMobileOAuthDisconnect")
}
func (h *routeRecorder) ReviewMobileOAuthDisconnectChallenge(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "ReviewMobileOAuthDisconnectChallenge")
}
func (h *routeRecorder) MobileOAuthConnectionVerification(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "MobileOAuthConnectionVerification")
}
func (h *routeRecorder) LoginUser(w http.ResponseWriter, _ *http.Request) { h.record(w, "LoginUser") }
func (h *routeRecorder) ValidateEmailVerificationCode(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "ValidateEmailVerificationCode")
}
func (h *routeRecorder) CreateUserAPIToken(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "CreateUserAPIToken")
}
func (h *routeRecorder) GetSpecificUserAPITokens(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "GetSpecificUserAPITokens")
}
func (h *routeRecorder) DeleteUserAPIToken(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "DeleteUserAPIToken")
}
func (h *routeRecorder) ActivateUserAPIToken(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "ActivateUserAPIToken")
}
func (h *routeRecorder) RevokeUserAPIToken(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "RevokeUserAPIToken")
}
func (h *routeRecorder) GetUserAPITokenThreshold(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "GetUserAPITokenThreshold")
}
func (h *routeRecorder) LogoutUserOthers(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "LogoutUserOthers")
}
func (h *routeRecorder) UpdateUserEmail(w http.ResponseWriter, _ *http.Request) {
	h.record(w, "UpdateUserEmail")
}
