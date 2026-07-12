// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Phixsura/attune/internal/inbound"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Config is the decrypted shape of inbound_sources.config for a Slack source.
type Config struct {
	Version        int                     `json:"version"`
	TokenEncrypted []byte                  `json:"token_encrypted"`
	TeamID         string                  `json:"team_id"`
	TeamName       string                  `json:"team_name"`
	WorkspaceURL   string                  `json:"workspace_url"`
	ChannelID      string                  `json:"channel_id"`
	ChannelName    string                  `json:"channel_name"`
	ThreadCache    []slackThreadCacheEntry `json:"thread_cache,omitempty"`
}

// ConfigVersion is the only supported on-disk schema version.
const ConfigVersion = 1

type slackAuthInfo struct {
	TeamID       string
	TeamName     string
	WorkspaceURL string
}

type slackChannel struct {
	ID         string
	Name       string
	IsPrivate  bool
	IsArchived bool
	IsShared   bool
}

type slackMessage struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	User        string `json:"user"`
	BotID       string `json:"bot_id"`
	Text        string `json:"text"`
	Ts          string `json:"ts"`
	ThreadTS    string `json:"thread_ts"`
	ReplyCount  int    `json:"reply_count"`
	LatestReply string `json:"latest_reply"`
}

type slackConnInputs struct {
	BotToken  string
	ChannelID string
}

func parseConfig(raw []byte, secrets inbound.SecretStore) (Config, []byte, error) {
	decoded, err := secrets.Decrypt(raw)
	if err != nil {
		return Config{}, nil, err
	}
	var cfg Config
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		return Config{}, nil, err
	}
	if cfg.Version != ConfigVersion {
		return Config{}, nil, errors.New("slack: unsupported config version")
	}
	if cfg.ChannelID == "" {
		return Config{}, nil, errors.New("slack: channel_id missing")
	}
	if len(cfg.TokenEncrypted) == 0 {
		return Config{}, nil, errors.New("slack: token missing")
	}
	token, err := secrets.Decrypt(cfg.TokenEncrypted)
	if err != nil {
		return Config{}, nil, err
	}
	return cfg, token, nil
}

func validateSlackConnConfig(cfg *attunev1.SlackConnConfig, requireChannel bool) (slackConnInputs, error) {
	if cfg == nil {
		return slackConnInputs{}, errors.New("slack_config is required")
	}
	out := slackConnInputs{
		BotToken:  strings.TrimSpace(cfg.GetBotToken()),
		ChannelID: strings.TrimSpace(cfg.GetChannelId()),
	}
	if out.BotToken == "" {
		return out, errors.New("slack_config.bot_token must not be empty")
	}
	if requireChannel && out.ChannelID == "" {
		return out, errors.New("slack_config.channel_id must not be empty")
	}
	return out, nil
}

func buildSlackSourceMeta(srcID, srcName string, auth slackAuthInfo, channel slackChannel, messageTs, threadTS, permalink, authorID, authorKind, messageKind string, replyCount int, latestReplyTS string) map[string]any {
	return map[string]any{
		inboundSourceIDKey:      srcID,
		inboundSourceNameKey:    srcName,
		"slack_team_id":         auth.TeamID,
		"slack_team_name":       auth.TeamName,
		"slack_workspace_url":   auth.WorkspaceURL,
		"slack_channel_id":      channel.ID,
		"slack_channel_name":    channel.Name,
		"slack_message_ts":      messageTs,
		"slack_thread_ts":       threadTS,
		"slack_permalink":       permalink,
		"slack_author_id":       authorID,
		"slack_author_kind":     authorKind,
		"slack_message_kind":    messageKind,
		"slack_reply_count":     replyCount,
		"slack_latest_reply_ts": strings.TrimSpace(latestReplyTS),
	}
}

const (
	inboundSourceIDKey   = "inbound_source_id"
	inboundSourceNameKey = "inbound_source_name"
)

func messageAuthor(msg slackMessage) (id, kind string) {
	switch {
	case strings.TrimSpace(msg.User) != "":
		return strings.TrimSpace(msg.User), "user"
	case strings.TrimSpace(msg.BotID) != "":
		return strings.TrimSpace(msg.BotID), "bot"
	default:
		return "", "unknown"
	}
}

func messageKind(msg slackMessage) string {
	if strings.TrimSpace(msg.ThreadTS) != "" && strings.TrimSpace(msg.ThreadTS) != strings.TrimSpace(msg.Ts) {
		return "reply"
	}
	return "root"
}

func messageThreadTS(msg slackMessage) string {
	if ts := strings.TrimSpace(msg.ThreadTS); ts != "" {
		return ts
	}
	return strings.TrimSpace(msg.Ts)
}

func normalizeMessageText(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "&amp;", "&")
	raw = strings.ReplaceAll(raw, "&lt;", "<")
	raw = strings.ReplaceAll(raw, "&gt;", ">")
	return raw
}

func messagePermalink(workspaceURL, channelID, rootTS, ts string) string {
	workspaceURL = strings.TrimRight(strings.TrimSpace(workspaceURL), "/")
	channelID = strings.TrimSpace(channelID)
	rootTS = strings.TrimSpace(rootTS)
	ts = strings.TrimSpace(ts)
	if workspaceURL == "" || channelID == "" || ts == "" {
		return ""
	}
	permalink := workspaceURL + "/archives/" + channelID + "/p" + slackTimestampSlug(ts)
	if rootTS != "" && rootTS != ts {
		permalink += "?thread_ts=" + rootTS + "&cid=" + channelID
	}
	return permalink
}

func slackTimestampSlug(ts string) string {
	sec, frac, ok := strings.Cut(strings.TrimSpace(ts), ".")
	if !ok {
		return sec + "000000"
	}
	if len(frac) > 6 {
		frac = frac[:6]
	}
	if len(frac) < 6 {
		frac += strings.Repeat("0", 6-len(frac))
	}
	return sec + frac
}

func slackTimestampMicros(ts string) (int64, error) {
	sec, frac, ok := strings.Cut(strings.TrimSpace(ts), ".")
	if !ok {
		frac = ""
	}
	if sec == "" {
		return 0, errors.New("slack: empty timestamp")
	}
	secMicros, err := parseInt64(sec)
	if err != nil {
		return 0, err
	}
	if len(frac) > 6 {
		frac = frac[:6]
	}
	if len(frac) < 6 {
		frac += strings.Repeat("0", 6-len(frac))
	}
	fracMicros, err := parseInt64(frac)
	if err != nil {
		return 0, err
	}
	return secMicros*1_000_000 + fracMicros, nil
}

func slackTimestampFromMicros(micros int64) string {
	if micros < 0 {
		micros = 0
	}
	sec := micros / 1_000_000
	frac := micros % 1_000_000
	return fmt.Sprintf("%d.%06d", sec, frac)
}

func parseInt64(raw string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
}
