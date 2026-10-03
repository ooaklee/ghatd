package contacter

// CreationReceipt returns an allowlisted public creation projection while
// preserving the direct Comms response shape. Administrative snapshots, linked
// contact IDs and future private fields are deliberately excluded. Meta remains
// the submission's host-defined metadata: adapters must not put private history
// into it. The returned map is detached at its top level; nested values remain
// read-only, as in the original submission contract.
func (c *Comms) CreationReceipt() *Comms {
	if c == nil {
		return nil
	}
	r := &Comms{Id: c.Id, NanoId: c.NanoId, FullName: c.FullName, Email: c.Email, Type: c.Type, ProvidedType: c.ProvidedType, Message: c.Message, UserId: c.UserId, UserLoggedIn: c.UserLoggedIn, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if c.Meta != nil {
		r.Meta = make(map[string]interface{}, len(c.Meta))
		for k, v := range c.Meta {
			r.Meta[k] = v
		}
	}
	return r
}
