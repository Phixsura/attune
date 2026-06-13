// SPDX-License-Identifier: Apache-2.0

// Package digest contains integration tests for the daily digest worker (#27):
// the scheduler claim/dedup ledger, window aggregation, render, and raw-webhook
// delivery, exercised end-to-end against a real PostgreSQL.
package digest
