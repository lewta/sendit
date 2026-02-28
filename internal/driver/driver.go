package driver

import (
	"context"

	"github.com/lewta/sendit/internal/task"
)

// Driver executes a single task and returns a result.
type Driver interface {
	Execute(ctx context.Context, t task.Task) task.Result
}
