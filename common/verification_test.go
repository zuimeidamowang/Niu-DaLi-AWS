package common

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteVerificationCodeIfMatchPreservesReplacement(t *testing.T) {
	key := "conditional-delete@example.com"
	purpose := PasswordResetPurpose
	RegisterVerificationCodeWithKey(key, "new-code", purpose)
	t.Cleanup(func() { DeleteKey(key, purpose) })

	assert.False(t, DeleteVerificationCodeIfMatch(key, "old-code", purpose))
	assert.True(t, VerifyCodeWithKey(key, "new-code", purpose))

	require.True(t, DeleteVerificationCodeIfMatch(key, "new-code", purpose))
	assert.False(t, VerifyCodeWithKey(key, "new-code", purpose))
}

func TestConsumeVerificationCodeWithKeyIsSingleUseConcurrently(t *testing.T) {
	key := "single-use@example.com"
	purpose := PasswordResetPurpose
	RegisterVerificationCodeWithKey(key, "one-time-code", purpose)
	t.Cleanup(func() { DeleteKey(key, purpose) })

	var consumed atomic.Int32
	var wait sync.WaitGroup
	start := make(chan struct{})
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			if ConsumeVerificationCodeWithKey(key, "one-time-code", purpose) {
				consumed.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()

	require.Equal(t, int32(1), consumed.Load())
	assert.False(t, ConsumeVerificationCodeWithKey(key, "one-time-code", purpose))
}
