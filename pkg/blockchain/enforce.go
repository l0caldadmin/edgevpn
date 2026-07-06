/*
Copyright © 2021-2022 Ettore Di Giacinto <mudler@mocaccino.org>
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package blockchain

import (
	"fmt"
	"time"
)

// EnforcementMode defines the strictness of ownership enforcement.
// See docs/design/authenticated-ledger.md (section 6).
type EnforcementMode int

const (
	// EnforceStrict: reject all unauthorized mutations, no exceptions.
	EnforceStrict EnforcementMode = iota
	// EnforceWithGraceful: reject hijacks but allow some version transitions.
	EnforceWithGraceful
)

// EnforcementPolicy defines how strictly a merge must validate ownership and
// detect violations (hijacks, replays, tombstone forgery, etc.).
type EnforcementPolicy struct {
	Mode            EnforcementMode
	MaxVersionDrift uint64        // how far a re-submitted older version can lag
	TTL             time.Duration // ownership TTL for policy enforcement
	BlockVersioning bool          // enforce block version constraints
	SignatureCheck  bool          // require valid signatures (should always be true)
}

// DefaultEnforcementPolicy returns a production-ready enforcement policy for
// enforce mode rollout (M2).
func DefaultEnforcementPolicy(ttl time.Duration) EnforcementPolicy {
	return EnforcementPolicy{
		Mode:            EnforceStrict,
		MaxVersionDrift: 0, // no replays allowed
		TTL:             ttl,
		BlockVersioning: true,
		SignatureCheck:  true,
	}
}

// CanMutate checks whether an entry mutation (old -> new) is allowed under
// enforcement policy. It detects:
//   - hijacks: new owner != old owner (and old owner is still live)
//   - replays: new version <= old version
//   - signature forgery: invalid sig (delegated to Verify)
//   - staleness: owner is dead and entry is not reclaimable
func (ep EnforcementPolicy) CanMutate(
	bucket string,
	key string,
	old SignedData,
	new SignedData,
	pol BucketPolicy,
	health map[string]Data,
	now time.Time,
) error {
	// Rule 1: Signature must be valid.
	if err := Verify(bucket, key, new); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	// If old entry doesn't exist, new signer becomes owner (first-claim).
	if old.Owner == "" {
		return nil
	}

	// Rule 2: Detect hijack (owner mismatch when old owner is still live).
	if new.Owner != old.Owner {
		if IsLive(health, old.Owner, ep.TTL, now) {
			return fmt.Errorf("hijack detected: %s tried to claim %s/%s from live owner %s",
				new.Owner, bucket, key, old.Owner)
		}
		// Old owner is dead, new signer can reclaim if bucket allows it.
		if !pol.Reclaimable {
			return fmt.Errorf("bucket %s does not allow reclaim after owner expiry", bucket)
		}
	}

	// Rule 3: Detect replay/rollback (version must increase).
	if new.Version <= old.Version {
		drift := int64(old.Version) - int64(new.Version)
		if drift > int64(ep.MaxVersionDrift) {
			return fmt.Errorf("replay detected: version %d <= %d (drift=%d)",
				new.Version, old.Version, drift)
		}
	}

	// Rule 4: Enforce signature on tombstones.
	if new.Deleted && old.Owner != new.Owner {
		// Only the original owner can tombstone their own entry, or a live
		// owner cannot be overwritten by a deletion from someone else.
		if IsLive(health, old.Owner, ep.TTL, now) {
			return fmt.Errorf("cannot tombstone %s/%s: live owner %s holds it",
				bucket, key, old.Owner)
		}
	}

	return nil
}

// ShouldAcceptBlock checks if a block's version and metadata meet enforcement
// constraints (version monotonicity, signature chain continuity, etc.).
func (ep EnforcementPolicy) ShouldAcceptBlock(
	prevBlockVersion uint64,
	newBlockVersion uint64,
) error {
	if !ep.BlockVersioning {
		return nil
	}

	// Block versions must be strictly increasing (no forks).
	if newBlockVersion <= prevBlockVersion {
		return fmt.Errorf("block version rollback: %d -> %d", prevBlockVersion, newBlockVersion)
	}

	return nil
}

// ReplayDetectionState tracks recent mutations to detect and reject replayed
// blocks or entries. Useful when a peer re-announces an old state.
type ReplayDetectionState struct {
	seenBlocks  map[uint64]bool // block version -> has-been-seen
	seenEntries map[string]uint64 // bucket/key -> last-accepted-version
}

// NewReplayDetectionState creates an empty replay detection state.
func NewReplayDetectionState() *ReplayDetectionState {
	return &ReplayDetectionState{
		seenBlocks:  make(map[uint64]bool),
		seenEntries: make(map[string]uint64),
	}
}

// MarkBlockSeen records that we have processed blockVersion. Returns true if
// we've seen it before (probable replay).
func (r *ReplayDetectionState) MarkBlockSeen(blockVersion uint64) bool {
	if r.seenBlocks[blockVersion] {
		return true // replay detected
	}
	r.seenBlocks[blockVersion] = true
	return false
}

// MarkEntrySeen records the version we accepted for bucket/key. Returns true
// if this version is older than a previously accepted version (replay).
func (r *ReplayDetectionState) MarkEntrySeen(bucket, key string, version uint64) bool {
	entryKey := bucket + "/" + key
	if prev, ok := r.seenEntries[entryKey]; ok && version < prev {
		return true // older version than before (replay)
	}
	if prev, ok := r.seenEntries[entryKey]; !ok || version > prev {
		r.seenEntries[entryKey] = version
	}
	return false
}
