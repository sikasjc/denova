package workspacechange

import "denova/internal/revisionfile"

// Revision returns the stable content revision used for optimistic concurrency.
// It delegates to the single canonical definition in revisionfile because these
// values are compared against revisions produced by the editor save path and the
// book file service; a second local hash implementation would let those drift.
func Revision(content []byte) string {
	return revisionfile.Revision(content)
}

func stateRevision(content []byte, exists bool) string {
	if !exists {
		return "missing"
	}
	return Revision(content)
}
