// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"encoding/json"
	"fmt"
)

// trigDoc is a trigger as a stored state carries it: the running type and
// every type's settings by id (TrigParams), so a stored setup reads back
// through the same settings every surface edits, whatever the stages'
// layout.
type trigDoc struct {
	Type    string                    `json:"type"`
	Configs map[string]map[string]any `json:"configs"`
	// Placed names the types the owner places from the UI model's settings
	// (TrigConfig.Placed).
	Placed []string `json:"placed,omitempty"`
}

// MarshalJSON is the trigger as a stored state carries it.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (t Trigger) MarshalJSON() ([]byte, error) {
	d := trigDoc{Type: t.running().String(), Configs: map[string]map[string]any{}}
	for tt := range NumTrigTypes {
		c := t.Configs[tt]
		if len(c.Stages) == 0 {
			continue
		}
		vals := map[string]any{}
		channels := channelsOf(c)
		for _, p := range TrigParams(tt) {
			if p.PerChannel {
				per := make([]any, channels)
				for ch := range channels {
					v, err := t.param(tt, p.ID, ch)
					if err != nil {
						return nil, err
					}
					per[ch] = docValue(p, v)
				}
				vals[p.ID] = per
				continue
			}
			v, err := t.param(tt, p.ID, 0)
			if err != nil {
				return nil, err
			}
			vals[p.ID] = docValue(p, v)
		}
		d.Configs[tt.String()] = vals
		if c.Placed {
			d.Placed = append(d.Placed, tt.String())
		}
	}
	return json.Marshal(d)
}

// channelsOf is the analog channels a configuration's pattern names.
// TRLC-LINKS: REQ-DSO-008
func channelsOf(c TrigConfig) int {
	n := 0
	for _, st := range c.Stages {
		if st.Pattern != nil {
			n = max(n, len(st.Pattern.Terms))
		}
	}
	return n
}

// docValue is a setting's value in a stored state.
// TRLC-LINKS: REQ-DSO-008
func docValue(p TrigParam, v ParamValue) any {
	switch p.Kind { //nolint:exhaustive // default: a number
	case ParamChoice, ParamSource, ParamStates:
		return v.Text
	case ParamBool:
		return v.Num != 0
	}
	return v.Num
}

// decode sets t from a stored trigger on a scope with channels analog
// channels: every type starts from its default and takes the settings
// the document names; a setting it does not know is left out, one whose
// value is not the setting's refuses the document.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-011
func (t *Trigger) decode(b []byte, channels int) error {
	var d trigDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	*t = Trigger{}
	t.Fill(channels)
	for name, vals := range d.Configs {
		tt, ok := TrigTypeNamed(name)
		if !ok {
			continue
		}
		for _, p := range TrigParams(tt) {
			raw, ok := vals[p.ID]
			if !ok {
				continue
			}
			if per, isList := raw.([]any); isList && p.PerChannel {
				for ch, x := range per {
					if ch < channels {
						if err := t.setDoc(tt, p, x, ch, channels); err != nil {
							return err
						}
					}
				}
				continue
			}
			if err := t.setDoc(tt, p, raw, 0, channels); err != nil {
				return err
			}
		}
	}
	for i := range t.Configs {
		t.Configs[i].Placed = false
	}
	for _, name := range d.Placed {
		if tt, ok := TrigTypeNamed(name); ok {
			t.Configs[tt].Placed = true
		}
	}
	if d.Type != "" {
		tt, ok := TrigTypeNamed(d.Type)
		if !ok {
			return fmt.Errorf("stored trigger type %q: not a trigger type", d.Type)
		}
		t.Type = tt
	}
	return nil
}

// setDoc sets one stored setting.
// TRLC-LINKS: REQ-DSO-008
func (t *Trigger) setDoc(tt TrigType, p TrigParam, x any, ch, channels int) error {
	v := ParamValue{Ch: ch}
	switch x := x.(type) {
	case float64:
		v.Num = x
	case string:
		v.Text = x
	case bool:
		v.Num = b2f(x)
	default:
		return fmt.Errorf("stored %s %s: %v is not a value", tt, p.ID, x)
	}
	return t.setParam(tt, p.ID, v, channels)
}

// UnmarshalJSON reads a stored state: the trigger's settings are read on
// the state's channels.
// TRLC-LINKS: REQ-DSO-008
func (s *State) UnmarshalJSON(b []byte) error {
	type plain State
	var raw struct {
		plain
		Trigger json.RawMessage
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*s = State(raw.plain)
	s.Trigger = Trigger{}
	if len(raw.Trigger) == 0 || string(raw.Trigger) == "null" {
		s.Trigger.Fill(len(s.Channels))
		return nil
	}
	return s.Trigger.decode(raw.Trigger, len(s.Channels))
}
