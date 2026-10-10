// Package emailmanagerhelper optionally composes resolved vendor accounts and
// local inbox capture for the owning EmailManager. Hosts supply secret/default
// resolution, environment policy and borrowed HTTP clients. BuildServices makes
// no network call; NewStandardEmailManager retains final route validation, send
// selection and receipt ownership. Hosts retain templates, consent and lifetime.
package emailmanagerhelper
