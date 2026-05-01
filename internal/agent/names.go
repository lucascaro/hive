package agent

import (
	"crypto/rand"
	"encoding/binary"
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

// randomIndex returns a uniformly-distributed index in [0, n) using
// crypto/rand. This is overkill for "pick a name" but keeps the package
// free of math/rand seed concerns and avoids needing a global seed.
func randomIndex(n int) int {
	if n <= 0 {
		return 0
	}
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	v := binary.BigEndian.Uint64(buf[:])
	return int(v % uint64(n))
}

// RandomName returns "<adjective>-<noun> <suffix>" where suffix is the
// agent's canonical ID (or "shell" when id is empty / unknown).
//
// Examples:
//   "amber-falcon claude"
//   "still-meadow shell"
func RandomName(id ID) string {
	suffix := string(id)
	if suffix == "" {
		suffix = "shell"
	}
	a := adjectives[randomIndex(len(adjectives))]
	n := nouns[randomIndex(len(nouns))]
	var b strings.Builder
	b.Grow(len(a) + 1 + len(n) + 1 + len(suffix))
	b.WriteString(a)
	b.WriteByte('-')
	b.WriteString(n)
	b.WriteByte(' ')
	b.WriteString(suffix)
	return b.String()
}
