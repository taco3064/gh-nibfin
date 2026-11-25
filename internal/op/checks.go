package op

// Check is one item as read back.
type Check struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Confirmed bool   `json:"confirmed"`
	// State is the state read back; Reason says why the item is not
	// confirmed.
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Result keys every changing command sets.
const (
	ResultItems = "items"
	ResultNotes = "notes"
)

// Checks collects the read-back items of an operation in a fixed order.
// Expect lists an item before it is read, so an item a failed read never
// reached is still reported, not confirmed.
type Checks struct {
	list []Check
	read []bool
}

func (c *Checks) index(id string) int {
	for i, x := range c.list {
		if x.ID == id {
			return i
		}
	}
	return -1
}

// Expect lists the items of steps as not yet read.
func (c *Checks) Expect(steps ...Step) {
	for _, s := range steps {
		if c.index(s.ID) < 0 {
			c.list = append(c.list, Check{ID: s.ID, Action: s.Action, Target: s.Target})
			c.read = append(c.read, false)
		}
	}
}

// Set records s as read back. A state that differs from the expectation is
// confirmed false with reason.
func (c *Checks) Set(s Step, confirmed bool, state, reason string) {
	x := Check{ID: s.ID, Action: s.Action, Target: s.Target, Confirmed: confirmed, State: state}
	if !confirmed {
		x.Reason = reason
		if x.Reason == "" {
			x.Reason = "not in the expected state"
		}
	}
	if i := c.index(s.ID); i >= 0 {
		c.list[i], c.read[i] = x, true
		return
	}
	c.list = append(c.list, x)
	c.read = append(c.read, true)
}

// Drop removes an expected item that turned out not to apply.
func (c *Checks) Drop(id string) {
	if i := c.index(id); i >= 0 {
		c.list = append(c.list[:i], c.list[i+1:]...)
		c.read = append(c.read[:i], c.read[i+1:]...)
	}
}

// List returns the items. An item not read is not confirmed, with err as
// the reason, or "not read back" when err is nil.
func (c *Checks) List(err error) []Check {
	out := make([]Check, len(c.list))
	for i, x := range c.list {
		if !c.read[i] {
			x.Confirmed, x.State = false, ""
			x.Reason = "not read back"
			if err != nil {
				x.Reason = "not read back: " + AsError(err).Error()
			}
		}
		out[i] = x
	}
	return out
}

// Fill sets the shared keys items and notes on r (a new Result when nil)
// and returns it.
func (c *Checks) Fill(r Result, notes []string, err error) Result {
	if r == nil {
		r = Result{}
	}
	if notes == nil {
		notes = []string{}
	}
	r[ResultItems] = c.List(err)
	r[ResultNotes] = notes
	return r
}
