// SPDX-License-Identifier: Apache-2.0

package inboundtest

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestAdapterContractWithStubAdapter(t *testing.T) {
	inbound.ResetForTest()
	t.Cleanup(inbound.ResetForTest)
	inbound.Register("contract-stub", "Contract Stub", func() inbound.Adapter {
		return ptrext.Of(contractStubAdapter{})
	})

	TestAdapterContract(t, func() inbound.Adapter {
		return ptrext.Of(contractStubAdapter{})
	})
}

type contractStubAdapter struct {
	started bool
}

func (a *contractStubAdapter) Channel() string {
	return "contract-stub"
}

func (a *contractStubAdapter) Start(context.Context, inbound.Deps) error {
	a.started = true
	return nil
}

func (a *contractStubAdapter) Shutdown(context.Context) error {
	a.started = false
	return nil
}
