// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package buildinfo

import "fmt"

// The Appropriate Legal Notices (GPLv3 §0, §5(d)). ADDITIONAL-TERMS.md
// term (a) requires the attribution to appear, unaltered, in every
// program's user interface. The GUI's copy lives in
// cmd/hivegui/frontend/src/lib/legal.ts; legal_test.go keeps the two equal.
const (
	Copyright   = "Copyright (C) 2026 Lucas Caro"
	LicenseName = "GNU General Public License v3.0 only (GPL-3.0-only), with additional terms under Section 7"
	Warranty    = "This program comes with ABSOLUTELY NO WARRANTY. It is free software licensed under the GNU GPL v3 with additional terms — see LICENSE and ADDITIONAL-TERMS.md."
	Attribution = "Based on Hive by Lucas Caro — https://github.com/lucascaro/hive"
)

// LegalNotice is the block every binary prints after its --version line.
func LegalNotice() string {
	return fmt.Sprintf("%s\nLicense: %s\n%s\n%s\n", Copyright, LicenseName, Warranty, Attribution)
}
