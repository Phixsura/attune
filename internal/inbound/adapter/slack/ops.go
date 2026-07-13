// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"errors"
	"strings"
)

func findChannel(channels []slackChannel, channelID string) (slackChannel, bool) {
	channelID = strings.TrimSpace(channelID)
	for _, ch := range channels {
		if strings.EqualFold(ch.ID, channelID) {
			return ch, true
		}
	}
	return slackChannel{}, false
}

func isPermanentSlackError(err error) bool {
	var apiErr apiError
	if errors.As(err, &apiErr) {
		return apiErr.Permanent()
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"invalid_auth",
		"not_authed",
		"token_revoked",
		"account_inactive",
		"missing_scope",
		"channel_not_found",
		"not_in_channel",
		"no_permission",
		"access_denied",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func isSlackDuplicateError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "idempotency key used with different request")
}

func isSlackThreadNotFoundError(err error) bool {
	var apiErr apiError
	if errors.As(err, &apiErr) {
		return apiErr.code == "thread_not_found"
	}
	return strings.Contains(strings.ToLower(err.Error()), "thread_not_found")
}
