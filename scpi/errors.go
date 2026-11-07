package scpi

import "strconv"

// Standard SCPI-99 error codes the engine and handlers use.
const (
	ErrNone               = 0
	ErrInvalidCharacter   = -101
	ErrSyntax             = -102
	ErrDataType           = -104
	ErrParamNotAllowed    = -108
	ErrMissingParam       = -109
	ErrHeader             = -110
	ErrHeaderSeparator    = -111
	ErrUndefinedHeader    = -113
	ErrHeaderSuffixRange  = -114
	ErrInvalidSuffix      = -131
	ErrExecution          = -200
	ErrSettingsConflict   = -221
	ErrDataOutOfRange     = -222
	ErrIllegalParamValue  = -224
	ErrQueueOverflow      = -350
	ErrInputBufferOverrun = -363
)

// StandardErrors is the SCPI-99 error list (volume 2, §21.8). A dialect may
// replace it with the texts its instruments are known to use.
var StandardErrors = map[int]string{
	0:    "No error",
	-100: "Command error",
	-101: "Invalid character",
	-102: "Syntax error",
	-103: "Invalid separator",
	-104: "Data type error",
	-105: "GET not allowed",
	-108: "Parameter not allowed",
	-109: "Missing parameter",
	-110: "Command header error",
	-111: "Header separator error",
	-112: "Program mnemonic too long",
	-113: "Undefined header",
	-114: "Header suffix out of range",
	-115: "Unexpected number of parameters",
	-120: "Numeric data error",
	-121: "Invalid character in number",
	-123: "Exponent too large",
	-124: "Too many digits",
	-128: "Numeric data not allowed",
	-130: "Suffix error",
	-131: "Invalid suffix",
	-134: "Suffix too long",
	-138: "Suffix not allowed",
	-140: "Character data error",
	-141: "Invalid character data",
	-144: "Character data too long",
	-148: "Character data not allowed",
	-150: "String data error",
	-151: "Invalid string data",
	-158: "String data not allowed",
	-160: "Block data error",
	-161: "Invalid block data",
	-168: "Block data not allowed",
	-170: "Expression error",
	-171: "Invalid expression",
	-178: "Expression data not allowed",
	-180: "Macro error",
	-181: "Invalid outside macro definition",
	-183: "Invalid inside macro definition",
	-184: "Macro parameter error",
	-200: "Execution error",
	-201: "Invalid while in local",
	-202: "Settings lost due to rtl",
	-203: "Command protected",
	-210: "Trigger error",
	-211: "Trigger ignored",
	-212: "Arm ignored",
	-213: "Init ignored",
	-214: "Trigger deadlock",
	-215: "Arm deadlock",
	-220: "Parameter error",
	-221: "Settings conflict",
	-222: "Data out of range",
	-223: "Too much data",
	-224: "Illegal parameter value",
	-225: "Out of memory",
	-226: "Lists not same length",
	-230: "Data corrupt or stale",
	-231: "Data questionable",
	-232: "Invalid format",
	-233: "Invalid version",
	-240: "Hardware error",
	-241: "Hardware missing",
	-250: "Mass storage error",
	-251: "Missing mass storage",
	-252: "Missing media",
	-253: "Corrupt media",
	-254: "Media full",
	-255: "Directory full",
	-256: "File name not found",
	-257: "File name error",
	-258: "Media protected",
	-260: "Expression error",
	-261: "Math error in expression",
	-270: "Macro error",
	-271: "Macro syntax error",
	-272: "Macro execution error",
	-273: "Illegal macro label",
	-274: "Macro parameter error",
	-275: "Macro definition too long",
	-276: "Macro recursion error",
	-277: "Macro redefinition not allowed",
	-278: "Macro header not found",
	-280: "Program error",
	-281: "Cannot create program",
	-282: "Illegal program name",
	-283: "Illegal variable name",
	-284: "Program currently running",
	-285: "Program syntax error",
	-286: "Program runtime error",
	-290: "Memory use error",
	-291: "Out of memory",
	-292: "Referenced name does not exist",
	-293: "Referenced name already exists",
	-294: "Incompatible type",
	-300: "Device-specific error",
	-310: "System error",
	-311: "Memory error",
	-312: "PUD memory lost",
	-313: "Calibration memory lost",
	-314: "Save/recall memory lost",
	-315: "Configuration memory lost",
	-320: "Storage fault",
	-321: "Out of memory",
	-330: "Self-test failed",
	-340: "Calibration failed",
	-350: "Queue overflow",
	-360: "Communication error",
	-361: "Parity error in program message",
	-362: "Framing error in program message",
	-363: "Input buffer overrun",
	-365: "Time out error",
	-400: "Query error",
	-410: "Query INTERRUPTED",
	-420: "Query UNTERMINATED",
	-430: "Query DEADLOCKED",
	-440: "Query UNTERMINATED after indefinite response",
	-500: "Power on",
	-600: "User request",
	-700: "Request control",
	-800: "Operation complete",
}

// queueSize is the number of real errors kept before the overflow marker.
const queueSize = 16

// queued is one error queue entry.
type queued struct {
	code int
	info string // device-dependent information, after ';'
}

// errorQueue is the FIFO error queue: 16 errors plus the overflow marker.
type errorQueue struct{ q []queued }

// push appends an error. A 17th error is discarded and -350 queued after the
// 16 kept; later errors are discarded while the queue is full.
// TRLC-LINKS: REQ-DSO-024
func (eq *errorQueue) push(e queued) {
	switch {
	case len(eq.q) < queueSize:
		eq.q = append(eq.q, e)
	case len(eq.q) == queueSize:
		eq.q = append(eq.q, queued{code: ErrQueueOverflow})
	}
}

// pop removes and returns the oldest entry; the zero entry when empty.
// TRLC-LINKS: REQ-DSO-024
func (eq *errorQueue) pop() queued {
	if len(eq.q) == 0 {
		return queued{}
	}
	e := eq.q[0]
	eq.q = eq.q[1:]
	return e
}

// TRLC-LINKS: REQ-DSO-024
func (eq *errorQueue) clear() { eq.q = nil }

// errorText is the text of code in the dialect's list; a code the list does
// not hold reads "Unknown error".
// TRLC-LINKS: REQ-DSO-024
func errorText(texts map[int]string, code int) string {
	if t, ok := texts[code]; ok {
		return t
	}
	return "Unknown error"
}

// format renders an entry as SYSTem:ERRor? answers: <code>,"<text>[;<info>]".
// TRLC-LINKS: REQ-DSO-024
func (e queued) format(texts map[int]string) string {
	t := errorText(texts, e.code)
	if e.info != "" {
		t += ";" + e.info
	}
	return strconv.Itoa(e.code) + ",\"" + t + "\""
}
