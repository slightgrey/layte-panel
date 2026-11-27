package sites

import (
	"context"

	"github.com/slightgrey/layte-panel/internal/agent"
)

// SetDiskQuota sets a Linux user's disk quota in MB (0 = none). Quotas come
// with the disk quota support; until then this is a no-op.
func (s *Service) SetDiskQuota(ctx context.Context, actor agent.Actor, user string, mb int) error {
	return nil
}
