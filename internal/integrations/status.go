// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

// Status is an integration's lock state, shown by `veduta integration list` and
// GET /api/v1/integrations - docs/01-architecture.md section 6's load flow:
//
//	load manifest -> compute sha256(canonical manifest bytes)
//	  -> compare against lock record
//	      match      -> run with the locked permissions
//	      no record  -> integration is `disabled`; UI and CLI offer approval
//	      mismatch   -> REFUSED, with a printed permission diff, until re-approved
type Status string

// Status values.
const (
	StatusBuiltin    Status = "builtin"    // source: builtin - exempt, no lock entry needed
	StatusApproved   Status = "approved"   // lock entry present and its digest matches
	StatusUnapproved Status = "unapproved" // no lock entry at all
	StatusChanged    Status = "changed"    // lock entry present but the digest no longer matches
)

// Evaluate reports an integration's Status. entry is nil when no lock record exists.
func Evaluate(manifest *Manifest, entry *LockEntry) Status {
	switch {
	case entry == nil:
		return StatusUnapproved
	case entry.ManifestSHA256 != manifest.Digest:
		return StatusChanged
	default:
		return StatusApproved
	}
}
