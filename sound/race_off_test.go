//go:build !race

// ENGMODEL-OWNER-UNIT: FU-DSO-SOUND

package sound

// raceBuild is false without the race detector (race_on_test.go).
const raceBuild = false
