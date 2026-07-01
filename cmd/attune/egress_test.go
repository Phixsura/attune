// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

func allowLoopbackEgressForTest(t *testing.T) {
	t.Helper()
	notify.SetEgressPolicy(nethardening.Policy{AllowLoopback: true, AllowPrivate: true})
	t.Cleanup(func() {
		notify.SetEgressPolicy(nethardening.Policy{})
	})
}
