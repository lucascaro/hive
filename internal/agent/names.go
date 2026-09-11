package agent

import (
	"math/rand/v2"
	"strings"
)

// adjectives and nouns are small curated lists that produce short,
// distinct, mostly innocuous session names. Roughly 40 × 40 = 1600
// combinations — enough collision-resistance for typical use.
var adjectives = []string{
	"amber", "azure", "brisk", "calm", "clever", "crimson", "dewy", "dim",
	"early", "ember", "fern", "fleet", "frost", "gentle", "golden", "happy",
	"hazel", "honey", "iron", "jade", "lazy", "lively", "lucky", "merry",
	"misty", "mossy", "nimble", "noble", "olive", "pale", "plum", "quiet",
	"rapid", "ruby", "rustic", "silent", "silver", "sly", "snowy", "spry",
	"still", "sunny", "swift", "tame", "teal", "terse", "tiny", "vivid",
	"wild", "windy", "wise", "young", "zesty",
}

var nouns = []string{
	"acorn", "anchor", "arrow", "badge", "bay", "beacon", "bee", "berry",
	"branch", "breeze", "brook", "canyon", "cedar", "cliff", "cloud", "comet",
	"copper", "coral", "creek", "crest", "delta", "dusk", "dune", "ember",
	"falcon", "fern", "field", "finch", "fjord", "flame", "fog", "forest",
	"glade", "grove", "harbor", "haven", "heron", "hill", "ivy", "lake",
	"leaf", "lily", "lynx", "marsh", "meadow", "mist", "moor", "moss",
	"otter", "owl", "peak", "pine", "plume", "pond", "raven", "ridge",
	"river", "rock", "sage", "sand", "shore", "sky", "spruce", "stone",
	"storm", "stream", "thicket", "thorn", "tide", "tundra", "valley",
	"willow", "wind",
}

// randomIndex returns a uniformly-distributed index in [0, n).
func randomIndex(n int) int {
	if n <= 0 {
		return 0
	}
	return rand.IntN(n)
}

// RandomName returns "<adjective>-<noun>".
//
// The agent's id used to be appended, which made every surface that also
// SHOWS the agent say it twice — the GUI sidebar renders it as a coloured
// two-letter glyph beside the name. Stripping it back out at display time
// was worse: nothing on the wire distinguishes a generated name from one
// the user typed, so a session deliberately named "weekly claude" lost a
// word. The name simply does not carry the agent any more.
//
// Names already stored keep whatever they were created with; this is the
// name a NEW session gets.
//
// Examples:
//
//	"amber-falcon"
//	"still-meadow"
func RandomName(ID) string {
	a := adjectives[randomIndex(len(adjectives))]
	n := nouns[randomIndex(len(nouns))]
	var b strings.Builder
	b.Grow(len(a) + 1 + len(n))
	b.WriteString(a)
	b.WriteByte('-')
	b.WriteString(n)
	return b.String()
}
