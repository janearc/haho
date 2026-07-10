package filter

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// RTKFilter pipes message content through the `rtk` CLI binary.
// rtk is a high-performance CLI proxy that filters and summarizes
// system outputs before they reach LLM context.
type RTKFilter struct {
	// binaryPath is the path to the rtk binary.
	binaryPath string
	// args are additional arguments to pass to rtk.
	args []string
}

// NewRTKFilter creates an RTK filter. If binaryPath is empty, it looks up
// rtk via exec.LookPath. Additional args can be passed to configure rtk behavior.
func NewRTKFilter(binaryPath string, args ...string) (*RTKFilter, error) {
	if binaryPath == "" {
		p, err := exec.LookPath("rtk")
		if err != nil {
			return nil, fmt.Errorf("rtk binary not found: %w", err)
		}
		binaryPath = p
	}
	return &RTKFilter{
		binaryPath: binaryPath,
		args:       args,
	}, nil
}

// Name returns the filter's identifier.
func (f *RTKFilter) Name() string { return "rtk" }

// Process pipes msg.Content through rtk via stdin/stdout.
func (f *RTKFilter) Process(ctx context.Context, msg *Message) (*Message, error) {
	cmd := exec.CommandContext(ctx, f.binaryPath, f.args...)
	cmd.Stdin = strings.NewReader(msg.Content)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("rtk execution failed: %w: %s", err, stderr.String())
	}

	meta := make(map[string]string, len(msg.Metadata)+1)
	for k, v := range msg.Metadata {
		meta[k] = v
	}
	meta["filter.rtk.applied"] = "true"

	return &Message{
		Role:     msg.Role,
		Content:  stdout.String(),
		Metadata: meta,
	}, nil
}
