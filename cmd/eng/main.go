// Command eng is the shell of the Engineering OS (RFC-0008).
//
// It coordinates; it does not implement. Indexing, retrieval and
// classification belong to this repository's kernel, diagnostics and MCP
// belong to engineering-mcp, and reviewing belongs to the client. A
// developer should be able to reach all of it from here without knowing
// which repository does what.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/truelogics/ai-memory/internal/cli"
)

const version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stdout)
		os.Exit(1)
	}

	ctx := context.Background()
	cmd, args := os.Args[1], os.Args[2:]

	var err error
	switch cmd {
	case "version", "--version", "-v":
		fmt.Println("eng version " + version)
		return
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return

	// --- Repository -------------------------------------------------
	case "init":
		err = cli.Init(ctx, firstArgOr(args, "."), os.Stdout)
	case "index":
		err = cli.Index(ctx, firstArgOr(args, "."), os.Stdout)
	case "sync", "update":
		err = cli.Update(ctx, firstArgOr(args, "."), os.Stdout)
	case "status":
		err = cli.Status(ctx, firstArgOr(args, "."), os.Stdout)
	case "clean":
		fs := flag.NewFlagSet("clean", flag.ExitOnError)
		yes := fs.Bool("yes", false, "delete without asking")
		fs.Parse(args)
		err = cli.Clean(ctx, firstArgOr(fs.Args(), "."), os.Stdout, *yes)

	// --- Knowledge --------------------------------------------------
	case "search":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "usage: eng search <query>")
			os.Exit(1)
		}
		err = cli.Search(ctx, ".", strings.Join(args, " "), os.Stdout)
	case "ask":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "usage: eng ask <question>")
			os.Exit(1)
		}
		err = cli.Context(ctx, ".", strings.Join(args, " "), os.Stdout)
	case "context":
		fs := flag.NewFlagSet("context", flag.ExitOnError)
		task := fs.String("task", "", "the task to gather context for")
		fs.Parse(args)
		if strings.TrimSpace(*task) == "" {
			fmt.Fprintln(os.Stderr, `usage: eng context --task "<description>"`)
			os.Exit(1)
		}
		err = cli.Context(ctx, ".", *task, os.Stdout)

	// --- Workspace --------------------------------------------------
	case "workspace":
		err = runWorkspace(ctx, args)

	// --- Meaning ----------------------------------------------------
	case "taxonomy":
		err = runTaxonomy(ctx, args)

	// --- The platform -----------------------------------------------
	case "doctor":
		err = cli.Doctor(ctx, ".", os.Stdout, args)
	case "config":
		err = cli.Config(ctx, firstArgOr(args, "."), os.Stdout)
	case "review":
		fs := flag.NewFlagSet("review", flag.ExitOnError)
		noLaunch := fs.Bool("no-launch", false, "print the instructions without starting Claude Code")
		fs.Parse(args)
		err = cli.Review(ctx, ".", os.Stdout, !*noLaunch)

	case "add":
		fmt.Fprintln(os.Stderr, "eng add: replaced by `eng workspace attach <path>`")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "eng: unknown command %q\n\n", cmd)
		printUsage(os.Stderr)
		os.Exit(1)
	}

	if err != nil {
		// A subcommand that already printed its own diagnosis exits
		// non-zero without a second, vaguer line on top of it.
		if !cli.Silent(err) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

func firstArgOr(args []string, fallback string) string {
	if len(args) > 0 {
		return args[0]
	}
	return fallback
}

// printUsage groups commands by the question a developer is asking, not
// by the component that answers it. Which repository implements what is
// exactly the thing this CLI exists to stop people needing to know.
func printUsage(out *os.File) {
	fmt.Fprintln(out, `eng — the Engineering OS

Usage:
  eng <command> [args]

Getting started:
  init [path]              create a workspace here
  taxonomy                 what this repository's directories mean, and how to say so
  index [path]             index every repository in the workspace
  doctor                   check this machine end to end, and say what to fix
  review                   check the setup, then hand over to Claude Code

Everyday:
  update [path]            incremental re-index (alias: sync)
  status [path]            what is indexed, and whether a rulebook is present
  search <query>           ranked full-text search
  ask <question>           gather the engineering context for a question
  context --task "<...>"   the same, task-shaped

Workspace — one index over one or more repositories:
  workspace create [path]  create a workspace and register its own directory
  workspace attach <path>  attach a repository and index it
  workspace detach <path>  remove a repository and its documents
  workspace list           every attached repository
  workspace status         documents, rules, and what this workspace can answer

Meaning:
  taxonomy                 show the declared directory mappings
  taxonomy validate        check them, and what they would classify

Machine:
  config                   the effective configuration for this directory
  clean [path] --yes       delete the generated .eng/ directory
  version

Reviewing is done by Claude Code, indexing by this CLI, and the MCP tools by
engineering-mcp. `+"`eng doctor`"+` checks all three.`)
}

func runWorkspace(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printWorkspaceUsage()
		os.Exit(1)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "create":
		return cli.WorkspaceCreate(ctx, firstArgOr(rest, "."), os.Stdout)
	case "list":
		return cli.WorkspaceList(ctx, firstArgOr(rest, "."), os.Stdout)
	case "status":
		return cli.WorkspaceStatus(ctx, firstArgOr(rest, "."), os.Stdout)
	case "attach":
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, "usage: eng workspace attach <path>")
			os.Exit(1)
		}
		return cli.WorkspaceAttach(ctx, ".", rest[0], os.Stdout)
	case "detach":
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, "usage: eng workspace detach <path>")
			os.Exit(1)
		}
		return cli.WorkspaceDetach(ctx, ".", rest[0], os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "eng workspace: unknown command %q\n\n", sub)
		printWorkspaceUsage()
		os.Exit(1)
		return nil
	}
}

func printWorkspaceUsage() {
	fmt.Fprintln(os.Stderr, `usage: eng workspace <command>

  create [path]   create a workspace and register its own directory
  list            list every attached repository
  status          documents, rules, and what this workspace can answer
  attach <path>   attach a repository to this workspace and index it
  detach <path>   remove a repository and its documents

attach and detach act on the workspace in the current directory, so run
them from the workspace root.`)
}

func runTaxonomy(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return cli.Taxonomy(ctx, ".", os.Stdout)
	}
	switch args[0] {
	case "validate":
		return cli.TaxonomyValidate(ctx, ".", os.Stdout)
	case "suggest":
		// Deliberately not implemented. Guessing what a repository's
		// directories mean and presenting it as a suggestion is how a
		// wrong mapping gets accepted without anyone deciding it —
		// engineering:VALIDATION_PHASE_1.md is explicit that mappings are
		// never invented for a repository by someone who does not work in
		// it, and an inference from directory names is exactly that.
		return fmt.Errorf("taxonomy suggest: not implemented, on purpose — a taxonomy is a claim about what your\n" +
			"directories mean, and a guess presented as a suggestion gets accepted without being decided.\n" +
			"Run `eng taxonomy` for the canonical types and write the mappings yourself")
	default:
		return fmt.Errorf("eng taxonomy: unknown command %q — try `eng taxonomy` or `eng taxonomy validate`", args[0])
	}
}
