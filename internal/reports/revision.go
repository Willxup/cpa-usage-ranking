package reports

// Version is the committed revision and catalog-independent input digest.
type Version struct {
	Revision int64
	Digest   [32]byte
}

type Decision uint8

const (
	Replace Decision = iota
	Confirm
	Conflict
)

// DecideVersion is pure: it never persists a revision or modifies statistics.
func DecideVersion(current *Version, incoming Version) Decision {
	if current == nil || incoming.Revision > current.Revision {
		return Replace
	}
	if incoming.Revision == current.Revision && incoming.Digest == current.Digest {
		return Confirm
	}
	return Conflict
}
