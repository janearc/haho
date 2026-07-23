// haho — the operator-facing CLI (RFC section 8): a thin cobra wrapper
// over the client package and nothing more, on delightd's precedent.
// Results are the daemon's own JSON, verbatim, on stdout — pipeable —
// and diagnostics are structured JSON on stderr, per fleet rules.
//
// This replaces the pre-RFC proof-of-concept proxy that previously owned
// this name; the PoC's internal packages await their own retirement diff,
// but the binary called haho now speaks hahod, as the RFC says it does.
//
// Task specs are protojson or YAML (the RFC's job.yml form): YAML is
// converted to JSON and then strictly validated against haho.job.v1 —
// an off-contract spec never leaves the operator's machine.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	sigyaml "sigs.k8s.io/yaml"

	"github.com/janearc/haho/client"
	"github.com/janearc/haho/gen/jobpb"
)

var (
	log  = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	addr string
)

func main() {
	root := &cobra.Command{
		Use:           "haho",
		Short:         "task work through hahod (RFC section 8)",
		SilenceUsage:  true,
		SilenceErrors: true, // errors are logged structured below, once
	}
	root.PersistentFlags().StringVar(&addr, "addr", envOr("HAHO_ADDR", "http://127.0.0.1:8790"),
		"hahod base URL (env HAHO_ADDR)")

	root.AddCommand(
		&cobra.Command{
			Use:   "new",
			Short: "create a quiescent session",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				id, err := client.Dial(addr).New(cmd.Context())
				if err != nil {
					return err
				}
				return printJSON(map[string]any{"id": id, "created": true})
			},
		},
		&cobra.Command{
			Use:   "task <session> <spec-file>",
			Short: "task a session (spec: protojson or YAML JobSpec)",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				raw, err := os.ReadFile(args[1])
				if err != nil {
					return err
				}
				// YAML is the authored form (the RFC's job.yml); JSON passes
				// through. Either way the bytes must survive strict protojson
				// before they travel — the client enforces that.
				if !json.Valid(raw) {
					raw, err = sigyaml.YAMLToJSON(raw)
					if err != nil {
						return fmt.Errorf("%s is neither protojson nor YAML: %w", args[1], err)
					}
				}
				status, err := client.Dial(addr).TaskRaw(cmd.Context(), args[0], raw)
				if err != nil {
					return err
				}
				return printJSON(map[string]any{"id": args[0], "accepted": true, "status": status})
			},
		},
		&cobra.Command{
			Use:   "status <session>",
			Short: "in_flight and disposition",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				inFlight, disposition, err := client.Dial(addr).Status(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return printJSON(map[string]any{"id": args[0], "in_flight": inFlight, "disposition": disposition})
			},
		},
		&cobra.Command{
			Use:   "completion <session>",
			Short: "the landed JobCompletion (protojson)",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				completion, err := client.Dial(addr).Completion(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return printProto(completion)
			},
		},
		&cobra.Command{
			Use:   "wait <session>",
			Short: "poll until landed, print the completion",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				completion, err := client.Dial(addr).Wait(cmd.Context(), args[0], 2*time.Second)
				if err != nil {
					return err
				}
				return printProto(completion)
			},
		},
	)

	if err := root.Execute(); err != nil {
		var refused *client.Refused
		if errors.As(err, &refused) {
			// A refusal is the daemon's structured no — logged as itself,
			// exit 2 so a wrapper can tell it from transport trouble.
			log.Error("refused", "code", refused.Code, "reason", refused.Reason)
			os.Exit(2)
		}
		log.Error("failed", "error", err.Error())
		os.Exit(1)
	}
}

// printJSON puts a result on stdout: results are data, pipeable, and the
// only thing stdout ever carries.
func printJSON(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

// printProto puts a contract message on stdout as protojson — the wire
// form is the printed form, byte-faithful to what landed.
func printProto(completion *jobpb.JobCompletion) error {
	raw, err := protojson.Marshal(completion)
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
