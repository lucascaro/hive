// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/proc"
)

// Runner runs an external command with a deadline and returns its
// stdout. Tests swap it out.
type Runner func(timeout time.Duration, name string, args ...string) (string, error)

// RunWithTimeout is the production Runner. The deadline is enforced by
// the context, so a command that outlives it is killed rather than
// abandoned. The error carries the command's stderr: the failures worth
// showing the user are written there. It goes through internal/proc so a
// Windows GUI never flashes a console window for it.
func RunWithTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := proc.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// Bound the wait for the output pipes after the kill: a grandchild that
	// inherited them would otherwise hold Run open past the deadline.
	cmd.WaitDelay = 500 * time.Millisecond
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%s: no answer after %s: %w", filepath.Base(name), timeout, context.DeadlineExceeded)
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return stdout.String(), nil
}
