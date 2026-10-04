// Package adminaccess adapts an existing HttpOnly browser session to one reviewed,
// email-confirmed token-allowance operation. It preserves the shared bearer API
// and live authority checks; it does not grant roles or general permissions.
package adminaccess
