//go:build linux

package layer

import (
	"context"
	"errors"
	"testing"
)

type testUploadWaiter struct {
	err   error
	waits int
}

func (w *testUploadWaiter) Wait(context.Context) error {
	w.waits++

	return w.err
}

func TestWaitForParentUploadConsumesSuccessfulUpload(t *testing.T) {
	t.Parallel()

	upload := &testUploadWaiter{}
	executor := &LayerExecutor{pendingUploads: map[string]uploadWaiter{"parent": upload}}

	if err := executor.waitForParentUpload(context.Background(), "parent"); err != nil {
		t.Fatalf("waitForParentUpload() error = %v", err)
	}
	if upload.waits != 1 {
		t.Fatalf("Wait called %d times, want 1", upload.waits)
	}
	if _, ok := executor.pendingUploads["parent"]; ok {
		t.Fatal("successful parent upload was not removed")
	}
}

func TestWaitForParentUploadKeepsFailedUploadForRetry(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("upload failed")
	upload := &testUploadWaiter{err: wantErr}
	executor := &LayerExecutor{pendingUploads: map[string]uploadWaiter{"parent": upload}}

	if err := executor.waitForParentUpload(context.Background(), "parent"); !errors.Is(err, wantErr) {
		t.Fatalf("waitForParentUpload() error = %v, want %v", err, wantErr)
	}
	if _, ok := executor.pendingUploads["parent"]; !ok {
		t.Fatal("failed parent upload was removed")
	}
}
