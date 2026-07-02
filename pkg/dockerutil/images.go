package dockerutil

import (
	"fmt"

	"github.com/ddev/ddev/pkg/util"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

// ImageExistsLocally determines if an image is available locally.
func ImageExistsLocally(imageName string) (bool, error) {
	ctx, apiClient, err := GetDockerClient()
	if err != nil {
		return false, err
	}

	// If inspect succeeds, we have an image.
	_, err = apiClient.ImageInspect(ctx, imageName)
	if err == nil {
		return true, nil
	}
	return false, nil
}

// FindImagesByLabels takes a map of label names and values and returns any Docker images which match all labels.
// danglingOnly is used to return only dangling images, otherwise return all of them, including dangling.
func FindImagesByLabels(labels map[string]string, danglingOnly bool) ([]image.Summary, error) {
	if len(labels) < 1 {
		return nil, fmt.Errorf("the provided list of labels was empty")
	}
	filterList := client.Filters{}
	for k, v := range labels {
		label := fmt.Sprintf("%s=%s", k, v)
		// If no value is specified, filter any value by the key.
		if v == "" {
			label = k
		}
		filterList.Add("label", label)
	}

	if danglingOnly {
		filterList.Add("dangling", "true")
	}

	ctx, apiClient, err := GetDockerClient()
	if err != nil {
		return nil, err
	}
	images, err := apiClient.ImageList(ctx, client.ImageListOptions{
		All:     true,
		Filters: filterList,
	})
	if err != nil {
		return nil, err
	}
	return images.Items, nil
}

// ImageID returns the content-addressable ID (sha256:...) of a local image,
// falling back to the tag name on any error.
func ImageID(imageName string) string {
	ctx, apiClient, err := GetDockerClient()
	if err != nil {
		return imageName
	}
	info, err := apiClient.ImageInspect(ctx, imageName)
	if err != nil {
		return imageName
	}
	return info.ID
}

// ImageConfigUser returns the USER configured in an image (Config.User),
// e.g. "ddev", "1000", or "1000:1000". An empty string means the image runs as
// root and the image has no baked-in user.
func ImageConfigUser(imageName string) (string, error) {
	ctx, apiClient, err := GetDockerClient()
	if err != nil {
		return "", err
	}
	info, err := apiClient.ImageInspect(ctx, imageName)
	if err != nil {
		return "", err
	}
	if info.Config == nil {
		return "", nil
	}
	return info.Config.User, nil
}

// ImageConfigLabel returns the value of a label from the image config, or an
// empty string if the label is absent.
func ImageConfigLabel(imageName string, key string) (string, error) {
	ctx, apiClient, err := GetDockerClient()
	if err != nil {
		return "", err
	}
	info, err := apiClient.ImageInspect(ctx, imageName)
	if err != nil {
		return "", err
	}
	if info.Config == nil {
		return "", nil
	}
	return info.Config.Labels[key], nil
}

// TagImage adds an additional tag (target) pointing at the same image as source.
// This is a zero-disk alias: it creates a new name for an existing image.
func TagImage(source string, target string) error {
	ctx, apiClient, err := GetDockerClient()
	if err != nil {
		return err
	}
	_, err = apiClient.ImageTag(ctx, client.ImageTagOptions{Source: source, Target: target})
	return err
}

// FindImagesByReference returns all local images whose tag matches the given
// reference filter (Docker filter syntax, e.g. "ddev/ddev-webserver:*-built").
func FindImagesByReference(ref string) ([]image.Summary, error) {
	filterList := client.Filters{}
	filterList.Add("reference", ref)
	ctx, apiClient, err := GetDockerClient()
	if err != nil {
		return nil, err
	}
	images, err := apiClient.ImageList(ctx, client.ImageListOptions{
		All:     false,
		Filters: filterList,
	})
	if err != nil {
		return nil, err
	}
	return images.Items, nil
}

// RemoveImage removes an image with force
func RemoveImage(tag string) error {
	ctx, apiClient, err := GetDockerClient()
	if err != nil {
		return err
	}
	_, err = apiClient.ImageInspect(ctx, tag)
	if err == nil {
		_, err = apiClient.ImageRemove(ctx, tag, client.ImageRemoveOptions{Force: true})

		if err == nil {
			util.Debug("Deleted Docker image %s", tag)
		} else {
			util.Warning("Unable to delete %s: %v", tag, err)
		}
	}
	return nil
}
