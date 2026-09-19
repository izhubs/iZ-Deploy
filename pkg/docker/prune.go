// Package docker provides an enterprise-grade Go SDK wrapper around the Docker Engine API.
package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/filters"
)

// DECISION: Prune dangling images immediately after successful container rollout.
// WHY: Low-spec VPS nodes (1 vCPU / 1GB RAM) suffer from rapid root disk exhaustion on repeated builds.
// TRADE-OFF: Deletes untagged intermediate layers that could slightly speed up identical rebuilds.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-005

// PruneDanglingImages deletes untagged container images to reclaim host disk space.
//
// Business rule: Only images with dangling=true are reclaimed; active tags are never touched.
//
// @ai-constraint: Never run system prune with all=true; it would wipe base images for other apps.
func (c *Client) PruneDanglingImages(ctx context.Context) (uint64, error) {
	pruneFilters := filters.NewArgs(filters.Arg("dangling", "true"))
	report, err := c.cli.ImagesPrune(ctx, pruneFilters)
	if err != nil {
		return 0, fmt.Errorf("failed pruning dangling images: %w", err)
	}
	return report.SpaceReclaimed, nil
}
