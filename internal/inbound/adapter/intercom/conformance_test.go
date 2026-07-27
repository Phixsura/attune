// SPDX-License-Identifier: Apache-2.0

package intercom_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/intercom"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
)

func TestIntercomAdapterContract(t *testing.T) {
	inboundtest.TestAdapterContract(t, func() inbound.Adapter {
		return intercom.NewAdapter()
	})
}
