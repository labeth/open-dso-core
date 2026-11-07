// ENGMODEL-OWNER-UNIT: FU-DSO-SUPERRES

package superres

// StackConfig is a hardware stacking session's setup: it selects edge-locked hits on one channel and the fine grid.
// A hit is placed at the interpolated Level crossing; PreSamples puts the
// window origin before it. Bins fine bins span Bins/Factor input samples.
// TRLC-LINKS: REQ-DSO-141
type StackConfig struct {
	Channel       uint8  `json:"channel"`
	Falling       bool   `json:"falling"`
	Level         uint8  `json:"level"`
	Hysteresis    uint8  `json:"hysteresis"`
	PreSamples    uint32 `json:"pre_samples"`
	MinSeparation uint32 `json:"min_separation"`
	Bins          int    `json:"bins"`
	Factor        int    `json:"factor"`
	ChannelMask   uint8  `json:"channel_mask"`
	// Template, when set, qualifies each hit: the sum of |align channel -
	// Template[k]| at crossing-TemplatePre+k*TemplateStride must not exceed
	// TemplateThreshold (ADR-STACKING-TEMPLATE-QUALIFIER).
	Template          []int `json:"template,omitempty"`
	TemplateStride    int   `json:"template_stride,omitempty"`
	TemplatePre       int   `json:"template_pre,omitempty"`
	TemplateThreshold int   `json:"template_threshold,omitempty"`
}

// StackRequest asks a scope's hardware stacker for Records fresh captures stacked at the current timebase, raw
// samples only. RecordWords, when nonzero, bounds each record's depth (the
// FPGA rescans the whole record once per 32-bin tile).
// TRLC-LINKS: REQ-DSO-141
type StackRequest struct {
	Stack       StackConfig `json:"stack"`
	Records     int         `json:"records"`
	RecordWords uint32      `json:"record_words,omitempty"`
	// Progress, when set, hears each finished record (on the engine's owner
	// loop: it must not block).
	Progress func(done, total int) `json:"-"`
}

// StackResult reports the stack and the bus traffic it needed. RawWords
// is what recalling the same records would have moved; BusReads is what the
// moments actually cost.
// TRLC-LINKS: REQ-DSO-141
type StackResult struct {
	Result    Result `json:"result"`
	Hits      uint32 `json:"hits"`
	Crossings uint64 `json:"crossings"`
	// Rejected counts template-qualifier rejections over the whole session.
	Rejected uint64  `json:"rejected"`
	Records  int     `json:"records"`
	SampleS  float64 `json:"sample_s"`
	Factor   int     `json:"factor"`
	BusReads uint64  `json:"bus_reads"`
	RawWords uint64  `json:"raw_words"`
	LoadS    float64 `json:"load_s"`
	StackS   float64 `json:"stack_s"`
	// Time split of StackS: arm-to-frozen captures, FPGA scans, uploads and downloads.
	CaptureS  float64 `json:"capture_s"`
	ScanS     float64 `json:"scan_s"`
	UploadS   float64 `json:"upload_s"`
	DownloadS float64 `json:"download_s"`
	// Stack is the ARM-side accumulation for local review rendering.
	Stack *Stack `json:"-"`
}
