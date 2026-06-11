// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	llmconfigrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	llmconfigsvc "github.com/Phixsura/attune/internal/service/llmconfig"
)

func runLLM(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: attune llm channels|abilities|routes <verb> [flags]")
	}
	switch args[0] {
	case "channels":
		return runLLMChannels(args[1:])
	case "abilities":
		return runLLMAbilities(args[1:])
	case "routes":
		return runLLMRoutes(args[1:])
	default:
		return fmt.Errorf("unknown llm resource %q", args[0])
	}
}

func runLLMChannels(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: attune llm channels list|get|create|update|delete|test")
	}
	switch args[0] {
	case "list":
		return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
			rows, err := svc.ListChannels(ctx)
			if err != nil {
				return err
			}
			return printJSON(channelViews(rows))
		})
	case "get":
		return runLLMChannelGet(args[1:])
	case "create":
		return runLLMChannelCreate(args[1:])
	case "update":
		return runLLMChannelUpdate(args[1:])
	case "delete":
		return runLLMChannelDelete(args[1:])
	case "test":
		return runLLMChannelTest(args[1:])
	default:
		return fmt.Errorf("unknown llm channels verb %q", args[0])
	}
}

func runLLMChannelGet(args []string) error {
	fs := flag.NewFlagSet("llm channels get", flag.ContinueOnError)
	id := fs.String("id", "", "channel UUID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	parsed, err := uuid.Parse(ptrext.Indirect(id))
	if err != nil {
		return fmt.Errorf("--id must be a UUID: %w", err)
	}
	return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
		row, err := svc.GetChannel(ctx, parsed)
		if err != nil {
			return err
		}
		return printJSON(channelView(ptrext.Indirect(row)))
	})
}

func runLLMChannelCreate(args []string) error {
	fs, input, apiKey, id := channelFlagSet("llm channels create")
	_ = id
	if err := fs.Parse(args); err != nil {
		return err
	}
	input.APIKey = apiKey.ptr()
	return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
		row, err := svc.CreateChannel(ctx, ptrext.Indirect(input))
		if err != nil {
			return err
		}
		return printJSON(channelView(ptrext.Indirect(row)))
	})
}

func runLLMChannelUpdate(args []string) error {
	fs, input, apiKey, id := channelFlagSet("llm channels update")
	if err := fs.Parse(args); err != nil {
		return err
	}
	setFlags := visitedFlags(fs)
	parsed, err := uuid.Parse(ptrext.Indirect(id))
	if err != nil {
		return fmt.Errorf("--id must be a UUID: %w", err)
	}
	return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
		existing, err := svc.GetChannel(ctx, parsed)
		if err != nil {
			return err
		}
		merged := channelInputFromExisting(ptrext.Indirect(existing))
		merged = applyChannelFlagOverrides(merged, ptrext.Indirect(input), setFlags)
		merged.APIKey = apiKey.ptr()
		row, err := svc.UpdateChannel(ctx, parsed, merged)
		if err != nil {
			return err
		}
		return printJSON(channelView(ptrext.Indirect(row)))
	})
}

func runLLMChannelDelete(args []string) error {
	fs := flag.NewFlagSet("llm channels delete", flag.ContinueOnError)
	id := fs.String("id", "", "channel UUID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	parsed, err := uuid.Parse(ptrext.Indirect(id))
	if err != nil {
		return fmt.Errorf("--id must be a UUID: %w", err)
	}
	return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
		return svc.DeleteChannel(ctx, parsed)
	})
}

func runLLMChannelTest(args []string) error {
	fs := flag.NewFlagSet("llm channels test", flag.ContinueOnError)
	id := fs.String("id", "", "channel UUID")
	providerModel := fs.String("provider-model", "", "provider model id (defaults to highest-priority enabled ability)")
	prompt := fs.String("prompt", "", "test prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	parsed, err := uuid.Parse(ptrext.Indirect(id))
	if err != nil {
		return fmt.Errorf("--id must be a UUID: %w", err)
	}
	return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
		result, err := svc.TestChannel(ctx, parsed, llmconfigsvc.TestChannelInput{
			ProviderModel: ptrext.Indirect(providerModel),
			Prompt:        ptrext.Indirect(prompt),
		})
		if result != nil {
			if printErr := printJSON(channelTestView(ptrext.Indirect(result), err == nil)); printErr != nil && err == nil {
				return printErr
			}
		}
		return err
	})
}

func channelFlagSet(name string) (*flag.FlagSet, *llmconfigsvc.ChannelInput, *optionalStringFlag, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	input := ptrext.Of(llmconfigsvc.ChannelInput{})
	id := fs.String("id", "", "channel UUID (update only)")
	apiKey := ptrext.Of(optionalStringFlag{})
	weight := ptrext.Of(1)
	timeoutSeconds := ptrext.Of(60)
	input.Weight = weight
	input.TimeoutSeconds = timeoutSeconds
	fs.Var(apiKey, "api-key", "write-only provider API key")
	fs.StringVar(&input.Name, "name", "", "channel name")
	fs.StringVar(&input.Protocol, "protocol", "", "openai-compat|openai-responses|anthropic|gemini")
	fs.StringVar(&input.BaseURL, "base-url", "", "provider base URL")
	fs.StringVar(&input.AuthMode, "auth-mode", "bearer", "bearer|none")
	fs.StringVar(&input.Status, "status", "enabled", "enabled|disabled|draining")
	fs.IntVar(&input.Priority, "priority", 0, "higher priority wins")
	fs.IntVar(weight, "weight", 1, "weighted selection inside equal priority")
	fs.IntVar(timeoutSeconds, "timeout-seconds", 60, "provider call timeout")
	return fs, input, apiKey, id
}

func visitedFlags(fs *flag.FlagSet) map[string]struct{} {
	out := map[string]struct{}{}
	fs.Visit(func(f *flag.Flag) {
		out[f.Name] = struct{}{}
	})
	return out
}

func channelInputFromExisting(ch llmconfigrepo.Channel) llmconfigsvc.ChannelInput {
	return llmconfigsvc.ChannelInput{
		Name:           ch.Name,
		Protocol:       ch.Protocol,
		BaseURL:        ch.BaseURL,
		AuthMode:       ch.AuthMode,
		Status:         ch.Status,
		Priority:       ch.Priority,
		Weight:         ptrext.Of(ch.Weight),
		TimeoutSeconds: ptrext.Of(ch.TimeoutSeconds),
	}
}

func applyChannelFlagOverrides(
	dst llmconfigsvc.ChannelInput,
	src llmconfigsvc.ChannelInput,
	set map[string]struct{},
) llmconfigsvc.ChannelInput {
	if _, ok := set["name"]; ok {
		dst.Name = src.Name
	}
	if _, ok := set["protocol"]; ok {
		dst.Protocol = src.Protocol
	}
	if _, ok := set["base-url"]; ok {
		dst.BaseURL = src.BaseURL
	}
	if _, ok := set["auth-mode"]; ok {
		dst.AuthMode = src.AuthMode
	}
	if _, ok := set["status"]; ok {
		dst.Status = src.Status
	}
	if _, ok := set["priority"]; ok {
		dst.Priority = src.Priority
	}
	if _, ok := set["weight"]; ok {
		dst.Weight = src.Weight
	}
	if _, ok := set["timeout-seconds"]; ok {
		dst.TimeoutSeconds = src.TimeoutSeconds
	}
	return dst
}

type optionalStringFlag struct {
	value string
	set   bool
}

func (f *optionalStringFlag) String() string {
	return f.value
}

func (f *optionalStringFlag) Set(v string) error {
	f.value = v
	f.set = true
	return nil
}

func (f *optionalStringFlag) ptr() *string {
	if f == nil || !f.set {
		return nil
	}
	return ptrext.Of(f.value)
}

func runLLMAbilities(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: attune llm abilities list|upsert|delete")
	}
	switch args[0] {
	case "list":
		return runLLMAbilitiesList(args[1:])
	case "upsert":
		return runLLMAbilityUpsert(args[1:])
	case "delete":
		return runLLMAbilityDelete(args[1:])
	default:
		return fmt.Errorf("unknown llm abilities verb %q", args[0])
	}
}

func runLLMAbilitiesList(args []string) error {
	fs := flag.NewFlagSet("llm abilities list", flag.ContinueOnError)
	channelID := fs.String("channel", "", "channel UUID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := uuid.Parse(ptrext.Indirect(channelID))
	if err != nil {
		return fmt.Errorf("--channel must be a UUID: %w", err)
	}
	return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
		rows, err := svc.ListAbilities(ctx, id)
		if err != nil {
			return err
		}
		return printJSON(rows)
	})
}

func runLLMAbilityUpsert(args []string) error {
	fs := flag.NewFlagSet("llm abilities upsert", flag.ContinueOnError)
	channelID := fs.String("channel", "", "channel UUID")
	input := ptrext.Of(llmconfigsvc.AbilityInput{Enabled: true})
	weight := ptrext.Of(1)
	input.Weight = weight
	fs.StringVar(&input.LogicalModel, "logical-model", "", "logical model id")
	fs.StringVar(&input.ProviderModel, "provider-model", "", "provider model id")
	fs.BoolVar(&input.Enabled, "enabled", true, "ability enabled")
	fs.IntVar(&input.Priority, "priority", 0, "higher priority wins")
	fs.IntVar(weight, "weight", 1, "weighted selection inside equal priority")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := uuid.Parse(ptrext.Indirect(channelID))
	if err != nil {
		return fmt.Errorf("--channel must be a UUID: %w", err)
	}
	return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
		row, err := svc.UpsertAbility(ctx, id, ptrext.Indirect(input))
		if err != nil {
			return err
		}
		return printJSON(row)
	})
}

func runLLMAbilityDelete(args []string) error {
	fs := flag.NewFlagSet("llm abilities delete", flag.ContinueOnError)
	channelID := fs.String("channel", "", "channel UUID")
	model := fs.String("logical-model", "", "logical model id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := uuid.Parse(ptrext.Indirect(channelID))
	if err != nil {
		return fmt.Errorf("--channel must be a UUID: %w", err)
	}
	return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
		return svc.DeleteAbility(ctx, id, ptrext.Indirect(model))
	})
}

func runLLMRoutes(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: attune llm routes list|upsert|delete")
	}
	switch args[0] {
	case "list":
		return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
			rows, err := svc.ListRoutes(ctx)
			if err != nil {
				return err
			}
			return printJSON(rows)
		})
	case "upsert":
		return runLLMRouteUpsert(args[1:])
	case "delete":
		return runLLMRouteDelete(args[1:])
	default:
		return fmt.Errorf("unknown llm routes verb %q", args[0])
	}
}

func runLLMRouteUpsert(args []string) error {
	fs := flag.NewFlagSet("llm routes upsert", flag.ContinueOnError)
	input := ptrext.Of(llmconfigsvc.RouteInput{Enabled: true})
	fs.StringVar(&input.TenantID, "tenant", "", "tenant id (empty = global)")
	fs.StringVar(&input.Purpose, "purpose", "enrich", "route purpose")
	fs.StringVar(&input.LogicalModel, "logical-model", "", "logical model id")
	fs.BoolVar(&input.Enabled, "enabled", true, "route enabled")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
		row, err := svc.UpsertRoute(ctx, ptrext.Indirect(input))
		if err != nil {
			return err
		}
		return printJSON(row)
	})
}

func runLLMRouteDelete(args []string) error {
	fs := flag.NewFlagSet("llm routes delete", flag.ContinueOnError)
	tenantID := fs.String("tenant", "", "tenant id (empty = global)")
	purpose := fs.String("purpose", "enrich", "route purpose")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withLLMConfigService(func(ctx context.Context, svc *llmconfigsvc.Service) error {
		return svc.DeleteRoute(ctx, ptrext.Indirect(tenantID), ptrext.Indirect(purpose))
	})
}

func withLLMConfigService(fn func(context.Context, *llmconfigsvc.Service) error) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()
	store, err := secretstore.NewTinkStoreFromJSONWithLegacy(
		cfg.Secrets.TinkKeyset,
		cfg.Secrets.LegacyInboundMasterKey,
	)
	if err != nil {
		return err
	}
	repo := llmconfigrepo.New(pool)
	svc := llmconfigsvc.NewService(repo, store)
	if err := svc.SyncKeyRegistry(ctx); err != nil {
		return err
	}
	return fn(ctx, svc)
}

type channelJSON struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	BaseURL         string `json:"base_url"`
	AuthMode        string `json:"auth_mode"`
	HasAPIKey       bool   `json:"has_api_key"`
	CredentialKeyID string `json:"credential_key_id"`
	Status          string `json:"status"`
	Priority        int    `json:"priority"`
	Weight          int    `json:"weight"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
}

type channelTestJSON struct {
	OK            bool        `json:"ok"`
	ProviderModel string      `json:"provider_model"`
	Text          string      `json:"text"`
	InputTokens   int32       `json:"input_tokens"`
	OutputTokens  int32       `json:"output_tokens"`
	LatencyMS     int64       `json:"latency_ms"`
	Channel       channelJSON `json:"channel"`
}

func channelViews(rows []llmconfigrepo.Channel) []channelJSON {
	out := make([]channelJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, channelView(row))
	}
	return out
}

func channelView(row llmconfigrepo.Channel) channelJSON {
	return channelJSON{
		ID:              row.ID.String(),
		Name:            row.Name,
		Protocol:        row.Protocol,
		BaseURL:         row.BaseURL,
		AuthMode:        row.AuthMode,
		HasAPIKey:       len(row.CredentialCiphertext) > 0,
		CredentialKeyID: row.CredentialKeyID,
		Status:          row.Status,
		Priority:        row.Priority,
		Weight:          row.Weight,
		TimeoutSeconds:  row.TimeoutSeconds,
	}
}

func channelTestView(row llmconfigsvc.ChannelTestResult, ok bool) channelTestJSON {
	return channelTestJSON{
		OK:            ok,
		ProviderModel: row.ProviderModel,
		Text:          row.Text,
		InputTokens:   row.InputTokens,
		OutputTokens:  row.OutputTokens,
		LatencyMS:     row.LatencyMS,
		Channel:       channelView(row.Channel),
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
