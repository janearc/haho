// haho — the operator-facing CLI (RFC section 8): a thin wrapper over the
// client package and nothing more. Verbs mirror the RFC's console
// walkthrough: new, task, status, completion, and wait as the polling
// convenience the walkthrough performs by hand. Output is the daemon's
// own JSON, verbatim — the CLI adds no vocabulary of its own.
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
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	sigyaml "sigs.k8s.io/yaml"

	"github.com/janearc/haho/client"
)

func usage() {
	fmt.Fprint(os.Stderr, `haho — task work through hahod (RFC section 8)

usage:
  haho new                          create a quiescent session
  haho task <session> <spec-file>   task it (spec: protojson or YAML JobSpec)
  haho status <session>             in_flight and disposition
  haho completion <session>         the landed JobCompletion (protojson)
  haho wait <session>               poll until landed, print the completion

flags:
  -addr <url>   hahod base URL (env HAHO_ADDR, default http://127.0.0.1:8790)

exit: 0 on success, 2 on a REFUSED admission, 1 on anything else.
`)
	os.Exit(1)
}

func main() {
	addr := flag.String("addr", envOr("HAHO_ADDR", "http://127.0.0.1:8790"),
		"hahod base URL (env HAHO_ADDR)")
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
	}

	c := client.Dial(*addr)
	ctx := context.Background()

	var err error
	switch args[0] {
	case "new":
		err = cmdNew(ctx, c)
	case "task":
		if len(args) != 3 {
			usage()
		}
		err = cmdTask(ctx, c, args[1], args[2])
	case "status":
		if len(args) != 2 {
			usage()
		}
		err = cmdStatus(ctx, c, args[1])
	case "completion":
		if len(args) != 2 {
			usage()
		}
		err = cmdCompletion(ctx, c, args[1])
	case "wait":
		if len(args) != 2 {
			usage()
		}
		err = cmdWait(ctx, c, args[1])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		var refused *client.Refused
		if errors.As(err, &refused) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func cmdNew(ctx context.Context, c *client.Client) error {
	id, err := c.New(ctx)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"id": id, "created": true})
}

func cmdTask(ctx context.Context, c *client.Client, session, specPath string) error {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	// YAML is the authored form (the RFC's job.yml); JSON passes through.
	// Either way the bytes must survive strict protojson before they
	// travel — the client enforces that.
	if !json.Valid(raw) {
		raw, err = sigyaml.YAMLToJSON(raw)
		if err != nil {
			return fmt.Errorf("%s is neither protojson nor YAML: %w", specPath, err)
		}
	}
	status, err := c.TaskRaw(ctx, session, raw)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"id": session, "accepted": true, "status": status})
}

func cmdStatus(ctx context.Context, c *client.Client, session string) error {
	inFlight, disposition, err := c.Status(ctx, session)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"id": session, "in_flight": inFlight, "disposition": disposition})
}

func cmdCompletion(ctx context.Context, c *client.Client, session string) error {
	completion, err := c.Completion(ctx, session)
	if err != nil {
		return err
	}
	raw, err := protojson.Marshal(completion)
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func cmdWait(ctx context.Context, c *client.Client, session string) error {
	completion, err := c.Wait(ctx, session, 2*time.Second)
	if err != nil {
		return err
	}
	raw, err := protojson.Marshal(completion)
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func printJSON(v any) error {
	raw, err := json.Marshal(v)
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
