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
	"io"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/l0caldadmin/edgevpn/pkg/protocol"
)

var _ = Describe("Observe mode acceptance", func() {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ttl := time.Minute
	ip := "10.1.0.1"

	newObserveLedger := func(warnings *int) *Ledger {
		return New(io.Discard, &MemoryStore{},
			WithOwnership(OwnershipObserve, DefaultRegistry(ttl), ttl),
			WithClock(func() time.Time { return now }),
			WithViolationLogger(func(string, ...interface{}) {
				*warnings++
			}),
		)
	}

	It("accepts a live-owner hijack in observe mode and records a violation", func() {
		warnings := 0
		l := newObserveLedger(&warnings)
		owner := newTestSigner()
		attacker := newTestSigner()

		feed(l, heartbeat(owner, now))
		feed(l, map[string]map[string]SignedData{
			protocol.MachinesLedgerKey: {ip: mkSignedEntry(owner, protocol.MachinesLedgerKey, ip, machine(owner.ID(), ip), 1, now)},
		})
		feed(l, map[string]map[string]SignedData{
			protocol.MachinesLedgerKey: {ip: mkSignedEntry(attacker, protocol.MachinesLedgerKey, ip, machine(attacker.ID(), ip), 2, now)},
		})

		Expect(storedOwner(l, protocol.MachinesLedgerKey, ip)).To(Equal(attacker.ID()))
		Expect(warnings).To(BeNumerically(">", 0))
	})

	It("does not warn on valid owner updates in observe mode", func() {
		warnings := 0
		l := newObserveLedger(&warnings)
		owner := newTestSigner()

		feed(l, heartbeat(owner, now))
		feed(l, map[string]map[string]SignedData{
			protocol.MachinesLedgerKey: {ip: mkSignedEntry(owner, protocol.MachinesLedgerKey, ip, machine(owner.ID(), ip), 1, now)},
		})
		feed(l, map[string]map[string]SignedData{
			protocol.MachinesLedgerKey: {ip: mkSignedEntry(owner, protocol.MachinesLedgerKey, ip, machine(owner.ID(), "10.1.0.2"), 2, now)},
		})

		v, found := l.GetKey(protocol.MachinesLedgerKey, ip)
		Expect(found).To(BeTrue())
		var got map[string]string
		Expect(v.Unmarshal(&got)).To(Succeed())
		Expect(got["Address"]).To(Equal("10.1.0.2"))
		Expect(warnings).To(BeZero())
	})

	It("applies cross-ledger unauthorized writes in observe mode", func() {
		aWarnings := 0
		bWarnings := 0
		a := newObserveLedger(&aWarnings)
		b := newObserveLedger(&bWarnings)
		owner := newTestSigner()
		attacker := newTestSigner()

		feed(a, heartbeat(owner, now))
		feed(a, map[string]map[string]SignedData{
			protocol.MachinesLedgerKey: {ip: mkSignedEntry(owner, protocol.MachinesLedgerKey, ip, machine(owner.ID(), ip), 1, now)},
		})
		Expect(b.Update(b, msgFor(a), nil)).To(Succeed())

		feed(a, map[string]map[string]SignedData{
			protocol.MachinesLedgerKey: {ip: mkSignedEntry(attacker, protocol.MachinesLedgerKey, ip, machine(attacker.ID(), "10.9.9.9"), 2, now)},
		})
		Expect(b.Update(b, msgFor(a), nil)).To(Succeed())

		Expect(storedOwner(b, protocol.MachinesLedgerKey, ip)).To(Equal(attacker.ID()))
		Expect(bWarnings).To(BeNumerically(">", 0))
	})
})
