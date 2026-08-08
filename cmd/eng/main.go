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
		printUsage()
		os.Exit(1)
	}

	ctx := context.Background()
	cmd, args := os.Args[1], os.Args[2:]

	var err error
	switch cmd {
	case "version":
		fmt.Println("eng version " + version)
		return
	case "init":
		err = cli.Init(ctx, firstArgOr(args, "."), os.Stdout)
	case "index":
		err = cli.Index(ctx, firstArgOr(args, "."), os.Stdout)
	case "sync":
		err = cli.Sync(ctx, firstArgOr(args, "."), os.Stdout)
	case "search":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "usage: eng search <query>")
			os.Exit(1)
		}
		err = cli.Search(ctx, ".", strings.Join(args, " "), os.Stdout)
	case "status":
		err = cli.Status(ctx, firstArgOr(args, "."), os.Stdout)
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
	case "workspace":
		err = runWorkspace(ctx, args)
	case "add":
		fmt.Fprintln(os.Stderr, "eng add: replaced by `eng workspace attach <path>`")
		os.Exit(1)
	case "doctor":
		fmt.Fprintf(os.Stderr, "eng %s: not yet implemented (see docs/cli/CLI.md)\n", cmd)
		os.Exit(1)
	default:
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func firstArgOr(args []string, fallback string) string {
	if len(args) > 0 {
		return args[0]
	}
	return fallback
}

func printUsage() {
	fmt.Println(`eng - Engineering Memory CLI

Usage:
  eng <command> [args]

Commands:
  version         print the eng version
  init [path]     bootstrap a workspace (default: current directory)
  index [path]    index markdown in a repository (default: current directory)
  sync [path]     incremental re-index using git (default: current directory)
  search <query>  ranked full-text search
  status [path]   report index health
  ask <question>  gather context for a question (Retriever + Context Builder)
  context --task "<description>"   same as ask, task-shaped instead of a question

Workspace (a workspace is one index over one or more repositories):
  workspace create [path]     create a workspace and register its own directory
  workspace list              list every attached repository
  workspace attach <path>     attach a repository and index it
  workspace detach <path>     remove a repository and its documents

Planned, not yet implemented (see docs/cli/CLI.md):
  doctor`)
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
		printWorkspaceUsage()
		os.Exit(1)
		return nil
	}
}

func printWorkspaceUsage() {
	fmt.Fprintln(os.Stderr, `usage: eng workspace <command>

  create [path]   create a workspace and register its own directory
  list            list every attached repository
  attach <path>   attach a repository to this workspace and index it
  detach <path>   remove a repository and its documents`)
}
