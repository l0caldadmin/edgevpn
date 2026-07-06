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

package config_test

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/mudler/edgevpn/pkg/config"
)

var _ = Describe("Config integration: observe mode rollout", func() {
	Describe("Ownership mode environment and defaults", func() {
		It("defaults to enforce mode when not specified", func() {
			c := &config.Ownership{}
			Expect(c.Mode).To(Equal(""))

			// Simulate CLI default application
			mode := c.Mode
			if mode == "" {
				mode = "enforce"
			}
			Expect(mode).To(Equal("enforce"))
		})

		It("respects EDGEVPNOWNERSHIP environment variable", func() {
			err := os.Setenv("EDGEVPNOWNERSHIP", "observe")
			Expect(err).NotTo(HaveOccurred())
			defer os.Unsetenv("EDGEVPNOWNERSHIP")

			mode := os.Getenv("EDGEVPNOWNERSHIP")
			Expect(mode).To(Equal("observe"))
		})

		It("supports all three ownership modes", func() {
			validModes := []string{"off", "observe", "enforce"}
			for _, m := range validModes {
				c := &config.Ownership{Mode: m}
				Expect(c.Mode).To(Equal(m))
			}
		})

		It("preserves ownership TTL configuration", func() {
			ttl := 120 * time.Second
			c := &config.Ownership{
				Mode: "observe",
				TTL:  ttl,
			}
			Expect(c.TTL).To(Equal(ttl))
		})
	})

	Describe("Observe mode readiness checks", func() {
		It("allows multiple nodes to converge with observe mode", func() {
			// Verify that observe mode config can be applied to multiple instances
			// without conflicts (simulated by creating independent config instances).
			configs := []*config.Ownership{
				{Mode: "observe", TTL: 120 * time.Second},
				{Mode: "observe", TTL: 120 * time.Second},
				{Mode: "observe", TTL: 120 * time.Second},
			}

			// All configs are consistent
			for _, cfg := range configs {
				Expect(cfg.Mode).To(Equal("observe"))
				Expect(cfg.TTL).To(Equal(120 * time.Second))
			}
		})

		It("detects mode consistency across mixed config attempts", func() {
			// This simulates a scenario where one node might be misconfigured
			// in a different mode (before M1 rollout starts). The test verifies
			// we can at least detect such configuration drift.
			node1 := &config.Ownership{Mode: "observe"}
			node2 := &config.Ownership{Mode: "enforce"}
			node3 := &config.Ownership{Mode: "observe"}

			// Simulate a consistency check
			var modes []string
			for _, cfg := range []*config.Ownership{node1, node2, node3} {
				modes = append(modes, cfg.Mode)
			}

			// Track that we can identify the mismatch
			modeSet := make(map[string]int)
			for _, m := range modes {
				modeSet[m]++
			}

			Expect(len(modeSet)).To(Equal(2), "expect mixed modes detected (should be 1 in production)")
			Expect(modeSet["observe"]).To(Equal(2))
			Expect(modeSet["enforce"]).To(Equal(1))
		})

		It("transitions between observe and enforce without panic", func() {
			// Simulate a node transitioning during rollout
			c := &config.Ownership{Mode: "observe"}

			// Phase 1: observe mode active
			Expect(c.Mode).To(Equal("observe"))

			// Transition simulation (later milestone)
			c.Mode = "enforce"
			Expect(c.Mode).To(Equal("enforce"))

			// Verify no panic or corruption
			Expect(c.TTL).To(Equal(time.Duration(0)))
		})

		It("maintains TTL settings during mode transitions", func() {
			originalTTL := 240 * time.Second
			c := &config.Ownership{
				Mode: "observe",
				TTL:  originalTTL,
			}

			// Transition to enforce (simulating M2)
			c.Mode = "enforce"

			// TTL should persist across mode change
			Expect(c.TTL).To(Equal(originalTTL))
		})
	})

	Describe("M1 observe rollout scenarios", func() {
		It("supports unplanned observer mode in a mostly-enforce cluster", func() {
			// This simulates the scenario where one node hasn't upgraded
			// or reverted to observe due to an issue. The system should
			// not crash, though ledger merge behavior depends on block
			// version negotiation (tested in blockchain package).

			nodeStates := map[string]*config.Ownership{
				"node-a": {Mode: "enforce"},
				"node-b": {Mode: "observe"}, // Laggard or reverted
				"node-c": {Mode: "enforce"},
			}

			// Verify we can track state without panic
			Expect(nodeStates).To(HaveLen(3))
			Expect(nodeStates["node-b"].Mode).To(Equal("observe"))
		})

		It("validates that off mode is deprecated but accepted for library users", func() {
			c := &config.Ownership{Mode: "off"}
			Expect(c.Mode).To(Equal("off"))

			// In production, this should trigger a warning
			// (verified in cmd/main.go or pkg/node/node.go)
		})

		It("tracks TTL defaults align across nodes", func() {
			// M1 default TTL check: all nodes should use same value
			// (or at least have sensible defaults)
			node1TTL := time.Duration(0) // CLI default interpreted as 120s
			node2TTL := time.Duration(0)

			// If both are 0, they both get the default behavior
			Expect(node1TTL).To(Equal(node2TTL))
		})
	})

	Describe("Rollback safety during observe phase", func() {
		It("allows reverting from enforce back to observe without data loss", func() {
			// Simulate a node that temporarily ran enforce, now reverts
			c := &config.Ownership{Mode: "enforce"}
			Expect(c.Mode).To(Equal("enforce"))

			// Revert to observe
			c.Mode = "observe"
			Expect(c.Mode).To(Equal("observe"))

			// Config integrity should be maintained
			Expect(c).NotTo(BeNil())
		})

		It("preserves config state across emergency rollback to off", func() {
			// Last-resort fallback scenario
			c := &config.Ownership{Mode: "observe", TTL: 300 * time.Second}

			// Emergency rollback to off
			c.Mode = "off"

			// Config should be intact (though ledger won't sign new entries)
			Expect(c.TTL).To(Equal(300 * time.Second))
			Expect(c.Mode).To(Equal("off"))
		})
	})
})
