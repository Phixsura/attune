package checks

import (
	"context"

	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/preflight"
)

func init() {
	preflight.Register(preflight.Check{
		Name:     "encryption:tink_keyset",
		Category: preflight.CategoryEncryption,
		Run:      checkTinkKeyset,
	})
}

func checkTinkKeyset(_ context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "encryption:tink_keyset",
		Category: preflight.CategoryEncryption,
	}
	if env.Cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		return r
	}
	raw := env.Cfg.Secrets.TinkKeyset
	if raw == "" {
		r.Status = preflight.StatusFail
		r.Message = "Tink keyset not configured"
		r.Remediation = "Set secrets.tink_keyset to a valid Tink JSON keyset. Generate one with: attune secrets generate-keyset"
		return r
	}

	store, err := secretstore.NewTinkStoreFromJSON(raw)
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = "Tink keyset load failed"
		r.Remediation = "The tink_keyset value is not a valid Tink JSON keyset. Generate a new one with: attune secrets generate-keyset"
		return r
	}

	const testPlaintext = "attune-preflight-test"
	ct, err := store.Encrypt([]byte(testPlaintext))
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = "Tink encrypt failed"
		r.Remediation = "The Tink keyset loaded but cannot encrypt. Regenerate with: attune secrets generate-keyset"
		return r
	}
	pt, err := store.Decrypt(ct)
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = "Tink decrypt failed"
		r.Remediation = "The Tink keyset can encrypt but not decrypt. This suggests a corrupt keyset."
		return r
	}
	if string(pt) != testPlaintext {
		r.Status = preflight.StatusFail
		r.Message = "Tink round-trip mismatch"
		r.Remediation = "Encrypt/decrypt round-trip produced different data. Regenerate keyset."
		return r
	}

	r.Status = preflight.StatusPass
	r.Message = "Encrypt/decrypt round-trip OK"
	return r
}
