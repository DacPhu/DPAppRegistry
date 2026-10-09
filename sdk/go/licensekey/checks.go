package licensekey

import "time"

// Expired reports whether the license term ended before now.
func (l *License) Expired(now time.Time) bool {
	return !l.ExpiresAt.IsZero() && now.After(l.ExpiresAt)
}

// UpdatesCover reports whether the license entitles its holder to a release
// published at publishedAt. A release with no date, or a license with no
// updates window, is covered.
func (l *License) UpdatesCover(publishedAt time.Time) bool {
	if publishedAt.IsZero() || l.UpdatesUntil.IsZero() {
		return true
	}
	return !publishedAt.After(l.UpdatesUntil)
}

// AllowsCluster reports whether the license may be used on the cluster with
// this ID. A license that names no clusters may be used on any.
func (l *License) AllowsCluster(id string) bool {
	if len(l.ClusterIDs) == 0 {
		return true
	}
	for _, allowed := range l.ClusterIDs {
		if allowed == id {
			return true
		}
	}
	return false
}

// Valid returns nil when the license is in term and allowed on clusterID,
// else ErrExpired or ErrWrongCluster.
func (l *License) Valid(now time.Time, clusterID string) error {
	if l.Expired(now) {
		return ErrExpired
	}
	if !l.AllowsCluster(clusterID) {
		return ErrWrongCluster
	}
	return nil
}
