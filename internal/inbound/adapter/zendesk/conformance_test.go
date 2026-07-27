// SPDX-License-Identifier: Apache-2.0

package zendesk_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/zendesk"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
)

func TestZendeskAdapterContract(t *testing.T) {
	inboundtest.TestAdapterContract(t, func() inbound.Adapter {
		return zendesk.NewAdapter()
	})
}
