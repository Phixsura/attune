// SPDX-License-Identifier: Apache-2.0

// Package ha provides integration tests for HA worker lease semantics.
// Tests verify that multi-replica deploys correctly handle:
//   - Concurrent task claiming (FOR UPDATE SKIP LOCKED)
//   - Fencing token enforcement (claimed_by checks)
//   - Heartbeat refresh with lease lost detection
//   - Graceful shutdown drain
//   - Stale claim recovery
//
// These tests require a real PostgreSQL database and simulate multiple
// worker instances competing for the same queue.
package ha
