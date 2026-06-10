package guardpolicy

import "github.com/google/uuid"

func parsePolicyID(raw string) (string, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
