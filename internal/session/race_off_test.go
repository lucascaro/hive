//go:build !race

package session

// raceEnabled mirrors testing/race detection at compile time so tests
// can branch on -race. The runtime/race import would also work, but a
// build-tag pair is simpler and adds no production dependency.
const raceEnabled = false
