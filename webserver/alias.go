// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver

// v1Aliases are the v1 per-channel control names (two channels, named by
// number) as the settings v2 control and channel they mean.
var v1Aliases = map[string]struct {
	name string
	ch   int
}{
	"vdiv1": {"vdiv", 0}, "vdiv2": {"vdiv", 1},
	"offset1": {"offset", 0}, "offset2": {"offset", 1},
	"probe1": {"probe", 0}, "probe2": {"probe", 1},
	"coupling1": {"coupling", 0}, "coupling2": {"coupling", 1},
	"bwl1": {"bwlimit", 0}, "bwl2": {"bwlimit", 1},
}

// PerChannel is the settings v2 controls that take a channel.
var PerChannel = map[string]bool{
	"vdiv": true, "offset": true, "probe": true, "coupling": true, "bwlimit": true, "invert": true,
}

// Translate turns a v1 control into its settings v2 control: vdiv1 is vdiv
// on channel 0, coupling2 coupling on channel 1. V1 keeps the name it came
// as. Every other control (one that is already v2, or a v1 name the v2 API
// keeps) is left as it is.
// TRLC-LINKS: REQ-DSO-162
func (c *Control) Translate() {
	a, ok := v1Aliases[c.Name]
	if !ok {
		return
	}
	c.V1, c.Name, c.Ch = c.Name, a.name, a.ch
}
