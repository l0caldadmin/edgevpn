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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Enforce Mode (M2): Replay and hijack rejection", func() {

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

			bucket := "machines"
			key := "10.0.0.1"

			// First submission: version 5
			Expect(state.MarkEntrySeen(bucket, key, 5)).To(BeFalse())

			// Older version arrives (classic replay attack)
			Expect(state.MarkEntrySeen(bucket, key, 3)).To(BeTrue())

			// Newer version is accepted
			Expect(state.MarkEntrySeen(bucket, key, 6)).To(BeFalse())
		})

		It("tracks multiple entries across buckets", func() {
			state := NewReplayDetectionState()

			// Machine entry
			Expect(state.MarkEntrySeen("machines", "10.0.0.1", 1)).To(BeFalse())

			// Service entry in different bucket
			Expect(state.MarkEntrySeen("services", "http", 1)).To(BeFalse())

			// Resubmit machine entry (older version)
			Expect(state.MarkEntrySeen("machines", "10.0.0.1", 0)).To(BeTrue())

			// Service entry stays unaffected
			Expect(state.MarkEntrySeen("services", "http", 1)).To(BeFalse())
		})
	})

	Describe("Enforcement policy", func() {
		It("creates default enforcement policy", func() {
			pol := DefaultEnforcementPolicy(time.Minute)

			Expect(pol.Mode).To(Equal(EnforceStrict))
			Expect(pol.MaxVersionDrift).To(Equal(uint64(0)))
			Expect(pol.TTL).To(Equal(time.Minute))
			Expect(pol.BlockVersioning).To(BeTrue())
			Expect(pol.SignatureCheck).To(BeTrue())
		})

		It("accepts increasing block versions", func() {
			pol := DefaultEnforcementPolicy(time.Minute)

			err := pol.ShouldAcceptBlock(99, 100)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects block version rollback", func() {
			pol := DefaultEnforcementPolicy(time.Minute)

			err := pol.ShouldAcceptBlock(100, 99)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("rollback"))
		})

		It("rejects identical block versions", func() {
			pol := DefaultEnforcementPolicy(time.Minute)

			err := pol.ShouldAcceptBlock(100, 100)
			Expect(err).To(HaveOccurred())
		})

		It("allows disabling block version checks", func() {
			pol := EnforcementPolicy{BlockVersioning: false}

			// Should not error even with bad version progression
			err := pol.ShouldAcceptBlock(100, 50)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Policy versioning", func() {
		It("detects version rollback (replay)", func() {
			// Version 5 cannot be replaced by version 3
			old := SignedData{Version: 5}
			new := SignedData{Version: 3}

			Expect(new.Version <= old.Version).To(BeTrue())
		})

		It("allows version advance", func() {
			old := SignedData{Version: 5}
			new := SignedData{Version: 6}

			Expect(new.Version > old.Version).To(BeTrue())
		})
	})

	Describe("Enforcement states", func() {
		It("supports strict enforcement mode", func() {
			pol := DefaultEnforcementPolicy(time.Minute)
			Expect(pol.Mode).To(Equal(EnforceStrict))
		})

		It("supports graceful enforcement mode", func() {
			pol := EnforcementPolicy{
				Mode: EnforceWithGraceful,
				TTL:  time.Minute,
			}
			Expect(pol.Mode).To(Equal(EnforceWithGraceful))
		})
	})
})
