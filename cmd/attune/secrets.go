// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	rotationrepo "github.com/Phixsura/attune/internal/repo/secretrotation"
	rotationsvc "github.com/Phixsura/attune/internal/service/secretrotation"
)

func runSecrets(args []string) error {
	if len(args) == 0 {
		return secretsUsage()
	}
	switch args[0] {
	case "generate-keyset":
		return runSecretsGenerateKeyset(args[1:])
	case "keyset-info":
		return runSecretsKeysetInfo(args[1:])
	case "add-key":
		return runSecretsAddKey(args[1:])
	case "set-primary":
		return runSecretsSetPrimary(args[1:])
	case "delete-key":
		return runSecretsDeleteKey(args[1:])
	case "reencrypt":
		return runSecretsReencrypt(args[1:])
	case "retire-key":
		return runSecretsRetireKey(args[1:])
	default:
		return secretsUsage()
	}
}

func secretsUsage() error {
	return fmt.Errorf("usage: attune secrets generate-keyset|keyset-info|add-key|set-primary|delete-key|reencrypt|retire-key")
}

func runSecretsGenerateKeyset(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: attune secrets generate-keyset")
	}
	raw, err := secretstore.GenerateAES256GCMKeysetJSON()
	if err != nil {
		return err
	}
	fmt.Print(raw)
	return nil
}

func runSecretsKeysetInfo(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: attune secrets keyset-info")
	}
	raw, err := configuredTinkKeyset()
	if err != nil {
		return err
	}
	store, err := secretstore.NewTinkStoreFromJSON(raw)
	if err != nil {
		return err
	}
	return printJSON(struct {
		PrimaryKeyID string                    `json:"primary_key_id"`
		Keys         []secretstore.KeyMetadata `json:"keys"`
	}{
		PrimaryKeyID: store.PrimaryKeyID(),
		Keys:         store.Keys(),
	})
}

func runSecretsAddKey(args []string) error {
	fs := flag.NewFlagSet("secrets add-key", flag.ContinueOnError)
	primary := fs.Bool("primary", false, "make the new key primary immediately")
	if err := fs.Parse(args); err != nil {
		return err
	}
	raw, err := configuredTinkKeyset()
	if err != nil {
		return err
	}
	updated, _, err := secretstore.AddAES256GCMKeyToKeysetJSON(raw, ptrext.Indirect(primary))
	if err != nil {
		return err
	}
	fmt.Print(updated)
	return nil
}

func runSecretsSetPrimary(args []string) error {
	fs := flag.NewFlagSet("secrets set-primary", flag.ContinueOnError)
	keyID := fs.String("key-id", "", "Tink key id to make primary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if ptrext.Indirect(keyID) == "" {
		return fmt.Errorf("--key-id is required")
	}
	raw, err := configuredTinkKeyset()
	if err != nil {
		return err
	}
	updated, err := secretstore.SetPrimaryKeysetJSON(raw, ptrext.Indirect(keyID))
	if err != nil {
		return err
	}
	fmt.Print(updated)
	return nil
}

func runSecretsDeleteKey(args []string) error {
	fs := flag.NewFlagSet("secrets delete-key", flag.ContinueOnError)
	keyID := fs.String("key-id", "", "Tink key id to remove from the local keyset JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if ptrext.Indirect(keyID) == "" {
		return fmt.Errorf("--key-id is required")
	}
	raw, err := configuredTinkKeyset()
	if err != nil {
		return err
	}
	updated, err := secretstore.DeleteKeyFromKeysetJSON(raw, ptrext.Indirect(keyID))
	if err != nil {
		return err
	}
	fmt.Print(updated)
	return nil
}

func runSecretsReencrypt(args []string) error {
	fs := flag.NewFlagSet("secrets reencrypt", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "commit DB updates; without this flag the command is a dry run")
	fromKeyID := fs.String("from-key-id", "", "only rewrite ciphertexts currently using this key id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withSecretRotationService(func(ctx context.Context, svc *rotationsvc.Service) error {
		report, err := svc.Reencrypt(ctx, rotationsvc.ReencryptOptions{
			Apply:     ptrext.Indirect(apply),
			FromKeyID: ptrext.Indirect(fromKeyID),
		})
		if err != nil {
			return err
		}
		return printJSON(report)
	})
}

func runSecretsRetireKey(args []string) error {
	fs := flag.NewFlagSet("secrets retire-key", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "commit registry retirement; without this flag the command is a dry run")
	keyID := fs.String("key-id", "", "Tink key id to retire")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if ptrext.Indirect(keyID) == "" {
		return fmt.Errorf("--key-id is required")
	}
	return withSecretRotationService(func(ctx context.Context, svc *rotationsvc.Service) error {
		report, err := svc.RetireKey(ctx, ptrext.Indirect(keyID), rotationsvc.RetireOptions{
			Apply: ptrext.Indirect(apply),
		})
		if err != nil {
			return err
		}
		return printJSON(report)
	})
}

func configuredTinkKeyset() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.Secrets.TinkKeyset, nil
}

func withSecretRotationService(fn func(context.Context, *rotationsvc.Service) error) error {
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
	return fn(ctx, rotationsvc.New(rotationrepo.New(pool), store))
}
