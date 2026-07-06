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
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/mudler/edgevpn/pkg/protocol"
)

var _ = Describe("Enforce Mode (M2): Replay and hijack rejection", func() {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ip := "10.2.0.1"

	Describe("Replay detection", func() {
		It("detects replayed blocks", func() {
			state := NewReplayDetectionState()

			// First submission of block version 100
			Expect(state.MarkBlockSeen(100)).To(BeFalse())

			// Re-submission of block version 100 (replay)
			Expect(state.MarkBlockSeen(100)).To(BeTrue())

			// New block version is accepted
			Expect(state.MarkBlockSeen(101)).To(BeFalse())
		})

		It("detects replayed entries within a block", func() {
			state := NewReplayDetectionState()

			bucket := protocol.MachinesLedgerKey
			key := ip

			// First submission: version 5
			Expect(state.MarkEntrySeen(bucket, key, 5)).To(BeFalse())

			// Re-submission of same version (replay)
			Expect(state.MarkEntrySeen(bucket, key, 5)).To(BeFalse()) // idempotent

			// Older version arrives (classic replay attack)
			Expect(state.MarkEntrySeen(bucket, key, 3)).To(BeTrue())

			// Newer version is accepted
			Expect(state.MarkEntrySeen(bucket, key, 6)).To(BeFalse())
		})

		It("tracks multiple entries across buckets", func() {
			state := NewReplayDetectionState()

			// Machine entry
			Expect(state.MarkEntrySeen(protocol.MachinesLedgerKey, "10.0.0.1", 1)).To(BeFalse())

			// Service entry in different bucket
			Expect(state.MarkEntrySeen(protocol.ServicesLedgerKey, "http", 1)).To(BeFalse())

			// Resubmit machine entry (old version)
			Expect(state.MarkEntrySeen(protocol.MachinesLedgerKey, "10.0.0.1", 0)).To(BeTrue())

			// Service entry stays unaffected
			Expect(state.MarkEntrySeen(protocol.ServicesLedgerKey, "http", 1)).To(BeFalse())
		})
	})

	Describe("Hijack rejection in enforce mode", func() {
		It("rejects hijack when attacker claims live owner's entry", func() {
			ep := DefaultEnforcementPolicy(time.Minute)

			a := newTestSigner()
			b := newTestSigner()
			now := time.Now()

			// A owns an entry
			old := mkSignedEntry(a, protocol.MachinesLedgerKey, ip, machine(a.ID(), ip), 1, now)

			// B tries to claim it
			new := mkSignedEntry(b, protocol.MachinesLedgerKey, ip, machine(b.ID(), ip), 2, now)

			// Health shows A is live
			health := map[string]Data{
				a.ID(): Data(now.UTC().Format(time.RFC3339)),
			}

			pol := DefaultRegistry(time.Minute)[protocol.MachinesLedgerKey]

			// Should reject hijack
			err := ep.CanMutate(protocol.MachinesLedgerKey, ip, old, new, pol, health, now)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("hijack detected"))
		})

		It("allows reclaim when original owner is dead", func() {
			ep := DefaultEnforcementPolicy(time.Minute)

			a := newTestSigner()
			b := newTestSigner()
			now := time.Now()

			// A owned the entry but is now expired (no entry in health)
			old := mkSignedEntry(a, protocol.MachinesLedgerKey, ip, machine(a.ID(), ip), 1, now)

			// B reclaims with higher version
			new := mkSignedEntry(b, protocol.MachinesLedgerKey, ip, machine(b.ID(), ip), 2, now)

			// Health is empty (A is not live)
			health := map[string]Data{}

			pol := DefaultRegistry(time.Minute)[protocol.MachinesLedgerKey]

			// Should allow reclaim
			err := ep.CanMutate(protocol.MachinesLedgerKey, ip, old, new, pol, health, now)
			Expect(err).NotTo(HaveOccurred())
		})

		It("detects attempted tombstone forgery", func() {
			ep := DefaultEnforcementPolicy(time.Minute)

			a := newTestSigner()
			b := newTestSigner()
			now := time.Now()

			// A owns the entry and is live
			old := mkSignedEntry(a, protocol.MachinesLedgerKey, ip, machine(a.ID(), ip), 1, now)

			// B tries to delete A's entry
			tombstone := mkSignedEntry(b, protocol.MachinesLedgerKey, ip, machine(b.ID(), ip), 2, now)
			tombstone.Deleted = true

			health := map[string]Data{
				a.ID(): Data(now.UTC().Format(time.RFC3339)),
			}

			pol := DefaultRegistry(time.Minute)[protocol.MachinesLedgerKey]

			// Should reject tombstone from non-owner of live entry
			err := ep.CanMutate(protocol.MachinesLedgerKey, ip, old, tombstone, pol, health, now)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot tombstone"))
		})

		It("rejects version rollback (replay attack)", func() {
			ep := DefaultEnforcementPolicy(time.Minute)

			a := newTestSigner()
			now := time.Now()

			// New version is 5
			old := mkSignedEntry(a, protocol.MachinesLedgerKey, ip, machine(a.ID(), ip), 5, now)

			// Attacker replays version 3
			replay := mkSignedEntry(a, protocol.MachinesLedgerKey, ip, machine(a.ID(), ip), 3, now)

			health := map[string]Data{
				a.ID(): Data(now.UTC().Format(time.RFC3339)),
			}

			pol := DefaultRegistry(time.Minute)[protocol.MachinesLedgerKey]

			// Should reject older version
			err := ep.CanMutate(protocol.MachinesLedgerKey, ip, old, replay, pol, health, now)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("replay detected"))
		})

		It("rejects entries with invalid signatures", func() {
			ep := DefaultEnforcementPolicy(time.Minute)

			a := newTestSigner()
			now := time.Now()

			old := mkSignedEntry(a, protocol.MachinesLedgerKey, ip, machine(a.ID(), ip), 1, now)

			// Craft a new entry with garbage signature
			new := mkSignedEntry(a, protocol.MachinesLedgerKey, ip, machine(a.ID(), ip), 2, now)
			new.Sig = []byte("garbage_signature")

			health := map[string]Data{
				a.ID(): Data(now.UTC().Format(time.RFC3339)),
			}

			pol := DefaultRegistry(time.Minute)[protocol.MachinesLedgerKey]

			// Should reject bad signature
			err := ep.CanMutate(protocol.MachinesLedgerKey, ip, old, new, pol, health, now)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("signature"))
		})

		It("allows first-claim by any peer", func() {
			ep := DefaultEnforcementPolicy(time.Minute)

			a := newTestSigner()
			now := time.Now()

			// Fresh entry, no previous owner
			old := SignedData{} // empty/no previous entry

			new := mkSignedEntry(a, protocol.MachinesLedgerKey, ip, machine(a.ID(), ip), 1, now)

			health := map[string]Data{}

			pol := DefaultRegistry(time.Minute)[protocol.MachinesLedgerKey]

			// Should allow first-claim
			err := ep.CanMutate(protocol.MachinesLedgerKey, ip, old, new, pol, health, now)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Block version enforcement", func() {
		It("rejects block version rollback", func() {
			ep := DefaultEnforcementPolicy(time.Minute)

			err := ep.ShouldAcceptBlock(100, 99)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("rollback"))
		})

		It("rejects identical block versions", func() {
			ep := DefaultEnforcementPolicy(time.Minute)

			err := ep.ShouldAcceptBlock(100, 100)
			Expect(err).To(HaveOccurred())
		})

		It("accepts increasing block versions", func() {
			ep := DefaultEnforcementPolicy(time.Minute)

			err := ep.ShouldAcceptBlock(99, 100)
			Expect(err).NotTo(HaveOccurred())
		})

		It("allows disabling block version checks", func() {
			ep := EnforcementPolicy{BlockVersioning: false}

			// Should not error even with bad version progression
			err := ep.ShouldAcceptBlock(100, 50)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Enforce mode with concurrent mutations", func() {
		It("serializes concurrent updates from same owner", func() {
			l := enforcedLedger(time.Minute, now)
			a := newTestSigner()

			// First update
			feed(l, heartbeat(a, now))
			feed(l, map[string]map[string]SignedData{
				protocol.MachinesLedgerKey: {ip: mkSignedEntry(a, protocol.MachinesLedgerKey, ip, machine(a.ID(), "10.0.0.1"), 1, now)},
			})

			// Concurrent update from same owner (higher version should win)
			now2 := now.Add(1 * time.Second)
			feed(l, map[string]map[string]SignedData{
				protocol.MachinesLedgerKey: {ip: mkSignedEntry(a, protocol.MachinesLedgerKey, ip, machine(a.ID(), "10.0.0.2"), 2, now2)},
			})

			v, _ := l.GetKey(protocol.MachinesLedgerKey, ip)
			var m map[string]string
			json.Unmarshal([]byte(v), &m)
			Expect(m["Address"]).To(Equal("10.0.0.2"))
		})

		It("rejects concurrent hijack attempts during enforce phase", func() {
			l := enforcedLedger(time.Minute, now)
			a := newTestSigner()
			b := newTestSigner()
			c := newTestSigner()

			// A claims the resource
			feed(l, heartbeat(a, now))
			feed(l, map[string]map[string]SignedData{
				protocol.MachinesLedgerKey: {ip: mkSignedEntry(a, protocol.MachinesLedgerKey, ip, machine(a.ID(), ip), 1, now)},
			})

			// B tries to hijack (should fail, A is live)
			feed(l, map[string]map[string]SignedData{
				protocol.MachinesLedgerKey: {ip: mkSignedEntry(b, protocol.MachinesLedgerKey, ip, machine(b.ID(), ip), 2, now)},
			})

			// C tries to hijack (should also fail)
			feed(l, map[string]map[string]SignedData{
				protocol.MachinesLedgerKey: {ip: mkSignedEntry(c, protocol.MachinesLedgerKey, ip, machine(c.ID(), ip), 3, now)},
			})

			// A still owns it
			Expect(storedOwner(l, protocol.MachinesLedgerKey, ip)).To(Equal(a.ID()))
		})
	})
})
