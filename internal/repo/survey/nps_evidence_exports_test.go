// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestNPSEvidenceArtifactDigestMatches(t *testing.T) {
	t.Parallel()

	artifact := []byte("report_version,run_id\n1,run-1\n")
	digest := sha256.Sum256(artifact)
	valid := fmt.Sprintf("sha256:%x", digest)

	if !npsEvidenceArtifactDigestMatches(artifact, valid) {
		t.Fatal("valid artifact digest was rejected")
	}
	if npsEvidenceArtifactDigestMatches(append(artifact, 'x'), valid) {
		t.Fatal("mutated artifact was accepted")
	}
	if npsEvidenceArtifactDigestMatches(artifact, "sha256:"+valid[7:len(valid)-1]+"0") {
		t.Fatal("mismatched artifact digest was accepted")
	}
}

func TestVerifyNPSCampaignRunEvidenceExportReturnsIntegrityError(t *testing.T) {
	t.Parallel()

	item := NPSCampaignRunEvidenceExport{
		Artifact:       []byte("stored bytes"),
		ArtifactSHA256: "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000",
	}
	if err := verifyNPSCampaignRunEvidenceExport(item); err == nil {
		t.Fatal("invalid artifact was accepted")
	}
}
