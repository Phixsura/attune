// Command ingest-cli is a small, real CLI built on the attune Go SDK. It submits
// one feedback item to an attune deployment and prints the stored row id.
//
// Usage:
//
//	export ATTUNE_BASE_URL=https://attune.example.com
//	export ATTUNE_API_KEY=att_sk_...
//	ingest-cli -content "the dashboard is slow" -source web -source-user u-42
//
// Content may also be piped on stdin:
//
//	echo "the dashboard is slow" | ingest-cli
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	attune "github.com/Phixsura/attune/sdk/go"
)

func main() {
	if err := run(); err != nil {
		var ae *attune.AttuneError
		if errors.As(err, &ae) {
			fmt.Fprintf(os.Stderr, "ingest failed: code=%s status=%d requestId=%s: %s\n",
				ae.Code, ae.Status, ae.RequestID, ae.Message)
		} else {
			fmt.Fprintf(os.Stderr, "ingest failed: %v\n", err)
		}
		os.Exit(1)
	}
}

func run() error {
	var (
		baseURL    = flag.String("url", os.Getenv("ATTUNE_BASE_URL"), "attune base URL (or ATTUNE_BASE_URL)")
		apiKey     = flag.String("api-key", os.Getenv("ATTUNE_API_KEY"), "ingest:write API key (or ATTUNE_API_KEY)")
		content    = flag.String("content", "", "feedback content (defaults to stdin)")
		source     = flag.String("source", "", "source channel (defaults to api)")
		sourceUser = flag.String("source-user", "", "opaque end-user identifier")
		pageURL    = flag.String("page-url", "", "originating page URL")
		idemKey    = flag.String("idempotency-key", "", "override the auto-generated idempotency key")
		timeout    = flag.Duration("timeout", 30*time.Second, "per-attempt request timeout")
	)
	flag.Parse()

	if *baseURL == "" || *apiKey == "" {
		return errors.New("set -url/-api-key or ATTUNE_BASE_URL/ATTUNE_API_KEY")
	}

	text := *content
	if text == "" {
		piped, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		text = strings.TrimSpace(string(piped))
	}
	if text == "" {
		return errors.New("no content: pass -content or pipe text on stdin")
	}

	client, err := attune.New(*baseURL, *apiKey, attune.WithTimeout(*timeout), attune.WithUserAgentSuffix("ingest-cli"))
	if err != nil {
		return err
	}

	var opts []attune.RequestOption
	if *idemKey != "" {
		opts = append(opts, attune.WithIdempotencyKey(*idemKey))
	}

	res, err := client.Ingest(context.Background(), attune.IngestInput{
		Content:    text,
		Source:     *source,
		SourceUser: *sourceUser,
		PageURL:    *pageURL,
	}, opts...)
	if err != nil {
		return err
	}

	return json.NewEncoder(os.Stdout).Encode(res)
}
