package e2e

import (
	"os"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/stretchr/testify/assert"
)

// resetRateLimitWorkspace clears the shared server state file and the local
// terraform state so a subsequent apply starts from a clean slate. Terraform
// destroy is not reliable for cleanup here: each terraform command is a fresh
// provider process with a fresh client-side limiter, so its first request can
// still land while the server's shared bucket is drained and get a 429.
func resetRateLimitWorkspace() {
	os.Remove("./gql-server/test.json")
	os.Remove("./test_rate_limit/terraform.tfstate")
	os.Remove("./test_rate_limit/terraform.tfstate.backup")
	// Drop the lock file so init regenerates it against the freshly built
	// provider; a stale checksum from a previous build breaks init.
	os.Remove("./test_rate_limit/.terraform.lock.hcl")
}

// TestRateLimitOffFailsOnPasses proves the client-side rate limiter is what
// keeps the provider under the server's limit. The test server rejects
// opted-in requests that exceed its limiter with a 429.
//
//   - With rate limiting disabled (the default), the provider bursts its
//     requests within a single apply, the server rejects one, and the apply
//     fails.
//   - With rate limiting enabled below the server's rate, every request in the
//     apply is paced far enough apart that the server accepts it, so the apply
//     succeeds where the unthrottled apply could not.
func TestRateLimitOffFailsOnPasses(t *testing.T) {
	resetRateLimitWorkspace()
	t.Cleanup(resetRateLimitWorkspace)
	assert.NoFileExists(t, "./gql-server/test.json")

	// Rate limiting disabled: the burst trips the server's limit and the
	// apply must fail with the server's 429 rejection.
	offOptions := &terraform.Options{
		TerraformDir: "./test_rate_limit",
		Vars:         map[string]interface{}{"rate_limit_per_second": 0},
		Logger:       logger.Discard,
	}
	_, offErr := terraform.InitAndApplyE(t, offOptions)
	assert.Error(t, offErr, "expected apply to fail without client-side rate limiting")
	if offErr != nil {
		assert.Contains(t, offErr.Error(), "rate limit exceeded",
			"expected the failure to be the server's rate limit rejection")
	}
	// Drop any partial state so the enabled run starts clean.
	resetRateLimitWorkspace()

	// Let the server's shared bucket refill after the disabled run's burst so
	// the enabled apply's first request cannot race a drained bucket.
	time.Sleep(2 * time.Second)

	// Rate limiting enabled (1 req/sec, below the server's ~2.5 req/sec
	// ceiling): the paced requests are all accepted and the apply succeeds.
	onOptions := &terraform.Options{
		TerraformDir: "./test_rate_limit",
		Vars: map[string]interface{}{
			"rate_limit_per_second": 1,
			"rate_limit_burst":      1,
		},
		Logger: logger.Discard,
	}
	_, onErr := terraform.InitAndApplyE(t, onOptions)
	assert.NoError(t, onErr, "expected apply to succeed with client-side rate limiting enabled")
	assert.FileExists(t, "./gql-server/test.json")

	// Best-effort teardown. A destroy is a fresh provider process whose first
	// request can race the server's shared bucket, so its result is not
	// asserted; t.Cleanup guarantees the workspace is reset regardless.
	terraform.DestroyE(t, onOptions)
}
