// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"context"
	"strings"
	"testing"
	"time"

	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

func TestReplyDrafterGenerateReturnsLoadErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	d := NewReplyDrafter(replydraftrepo.NewDraftTaskRepo(newUnreachableDraftWorkerPool(t)), nil)

	_, _, err := d.Generate(ctx, 42, "tenant-1")

	if err == nil || !strings.Contains(err.Error(), "load:") {
		t.Fatalf("Generate() error = %v, want load wrapper", err)
	}
}

func TestReplyDrafterPrecheckReturnsRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	d := NewReplyDrafter(replydraftrepo.NewDraftTaskRepo(newUnreachableDraftWorkerPool(t)), nil)

	status, enabled, found, lastGeneratedAt, err := d.Precheck(ctx, 42, "tenant-1")

	if err == nil {
		t.Fatal("Precheck() error = nil, want repo error")
	}
	if status != "" || enabled || found || lastGeneratedAt != nil {
		t.Fatalf(
			"Precheck() = (%q, %t, %t, %v, %v), want zero result with repo error",
			status,
			enabled,
			found,
			lastGeneratedAt,
			err,
		)
	}
}
