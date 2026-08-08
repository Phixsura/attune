// SPDX-License-Identifier: Apache-2.0

package survey

import "context"

const (
	staleInvitationExpirationDefaultLimit = 50
	staleInvitationExpirationMaxLimit     = 200
)

func (s *Service) ExpireStaleInvitations(ctx context.Context, limit int) (int, error) {
	if s == nil || s.repo == nil {
		return 0, ErrValidation
	}
	count, err := s.repo.ExpireStaleInvitations(ctx, boundedStaleInvitationExpirationLimit(limit), s.now().UTC(), "expired")
	return count, mapRepoError(err)
}

func boundedStaleInvitationExpirationLimit(limit int) int {
	if limit <= 0 {
		return staleInvitationExpirationDefaultLimit
	}
	if limit > staleInvitationExpirationMaxLimit {
		return staleInvitationExpirationMaxLimit
	}
	return limit
}
