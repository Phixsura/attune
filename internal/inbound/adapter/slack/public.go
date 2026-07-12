// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"context"
	"fmt"
	"strings"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Public aliases keep the adapter's runtime helpers reusable from the
// Console handler without re-declaring Slack payload shapes in a second
// package.
type (
	AuthInfo   = slackAuthInfo
	Channel    = slackChannel
	Message    = slackMessage
	ConnInputs = slackConnInputs
)

// ValidateConnConfig normalizes the Slack connection payload used by the
// Console create / test / discover flows.
func ValidateConnConfig(cfg *attunev1.SlackConnConfig, requireChannel bool) (ConnInputs, error) {
	return validateSlackConnConfig(cfg, requireChannel)
}

// AuthTest checks the token against Slack's auth.test endpoint and returns the
// resolved workspace identity.
func AuthTest(ctx context.Context, token string) (AuthInfo, error) {
	return newAPIClient(strings.TrimSpace(token)).AuthTest(ctx)
}

// Discover resolves auth.test + conversations.list, returning the readable
// channels for a token.
func Discover(ctx context.Context, token string) (AuthInfo, []Channel, error) {
	client := newAPIClient(strings.TrimSpace(token))
	auth, err := client.AuthTest(ctx)
	if err != nil {
		return AuthInfo{}, nil, err
	}
	channels, err := client.DiscoverChannels(ctx)
	if err != nil {
		return auth, nil, err
	}
	return auth, channels, nil
}

// ValidateChannel resolves the readable channel list and returns the selected
// channel if the token can actually read it.
func ValidateChannel(ctx context.Context, token, channelID string) (AuthInfo, Channel, error) {
	auth, channels, err := Discover(ctx, token)
	if err != nil {
		return AuthInfo{}, Channel{}, err
	}
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return auth, Channel{}, fmt.Errorf("slack_config.channel_id must not be empty")
	}
	channel, ok := findChannel(channels, channelID)
	if !ok {
		return auth, Channel{}, fmt.Errorf("slack channel %q is not readable from this token", channelID)
	}
	return auth, channel, nil
}

// IsPermanentError reports whether a Slack API error should disable the
// source instead of being retried.
func IsPermanentError(err error) bool {
	return isPermanentSlackError(err)
}
