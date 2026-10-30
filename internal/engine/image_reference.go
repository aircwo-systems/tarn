package engine

import (
	"context"
	"fmt"
	"strings"
)

// pinnedImageRepository holds a tag on every image a deployment is pinned to.
// With Docker's containerd image store an image that loses its last tag can
// no longer be found by ID, so without this a rebuilt tag would strand
// published function versions on a missing image.
const pinnedImageRepository = "tarn/pinned"

// PinnedImageTag is the Tarn-owned tag kept on a pinned image ID.
func PinnedImageTag(imageID string) string {
	return pinnedImageRepository + ":" + strings.Replace(imageID, ":", "-", 1)
}

// ResolveImageReference pins a Lambda deployment to immutable local image
// content, so retagging an image cannot change a published function version.
func (e *Engine) ResolveImageReference(ctx context.Context, reference string, architectures ...string) (string, error) {
	platform := ""
	if p := lambdaPlatform(architectures); p != nil {
		platform = p.OS + "/" + p.Architecture
	}
	if err := e.EnsureImagePlatform(ctx, reference, platform); err != nil {
		return "", err
	}
	image, err := e.client.ImageInspect(ctx, reference)
	if err != nil {
		return "", err
	}
	if err := e.client.ImageTag(ctx, image.ID, PinnedImageTag(image.ID)); err != nil {
		return "", fmt.Errorf("pin image %s: %w", reference, err)
	}
	return image.ID, nil
}
