package engine

import "context"

// ResolveImageReference pins a Lambda deployment to immutable local image
// content, so retagging an image cannot change a published function version.
func (e *Engine) ResolveImageReference(ctx context.Context, reference string) (string, error) {
	if err := e.EnsureImageRef(ctx, reference); err != nil {
		return "", err
	}
	image, err := e.client.ImageInspect(ctx, reference)
	if err != nil {
		return "", err
	}
	return image.ID, nil
}
