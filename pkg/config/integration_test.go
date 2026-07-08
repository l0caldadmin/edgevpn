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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/l0caldadmin/edgevpn/pkg/config"
)

var _ = Describe("Config integration: observe mode rollout", func() {
	Describe("Ownership mode integration through Config.ToOpts", func() {
		newCfg := func(mode string, ttl time.Duration) config.Config {
			return config.Config{
				NetworkToken: "test-token",
				Ownership: config.Ownership{
					Mode: mode,
					TTL:  ttl,
				},
			}
		}

		It("accepts empty ownership mode for programmatic configs", func() {
			_, _, err := newCfg("", 0).ToOpts(nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("accepts observe and enforce ownership modes", func() {
			for _, mode := range []string{"observe", "enforce"} {
				_, _, err := newCfg(mode, 120*time.Second).ToOpts(nil)
				Expect(err).NotTo(HaveOccurred(), "mode=%s should be accepted", mode)
			}
		})

		It("rejects invalid ownership modes via production option wiring", func() {
			_, _, err := newCfg("experimental", 120*time.Second).ToOpts(nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid ownership mode"))
		})
	})
})
