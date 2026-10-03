package importjob

import (
	"errors"
	"fmt"
	"shiftory-server/internal/ai"
	"testing"
)

func TestRetryablePreservesProcessingError(t *testing.T) {
	original := &ai.ProcessingError{Code: "PROVIDER_DEFERRED", Retryable: true}
	wrapped := fmt.Errorf("process: %w", Retryable(original))
	var actual *ai.ProcessingError
	if !IsRetryable(wrapped) || !errors.As(wrapped, &actual) || actual != original {
		t.Fatal("processing classification lost")
	}
}
