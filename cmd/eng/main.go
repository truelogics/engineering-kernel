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
	"runtime/debug"
	"strings"

	"github.com/truelogics/engineering-kernel/internal/cli"
)

// version is read from the build rather than hard-coded. The constant
// spelling said 0.1.0-dev through twelve sprints and one release, and
// then said 0.2.0 in a commit tagged v0.2.1 — a version string drifts
// from its tag by default, and there is no reason for a developer
// comparing two machines to be the one who notices.
//
// devVersion is what a `go build` from a working tree reports, since
// there is no module version to read in that case.
const devVersion = "dev"

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return devVersion
	}
	return info.Main.Version
}

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
		fmt.Println("eng version " + version())
		return
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return

	// --- Getting started --------------------------------------------
	case "setup":
		path, flags := splitFlags(args, "rules", "repo")
		fs := flag.NewFlagSet("setup", flag.ExitOnError)
		var opts cli.SetupOptions
		fs.Var(repeatable{&opts.Rules}, "rules", "path or git URL of your rules/ADR repository (repeatable)")
		fs.Var(repeatable{&opts.Repos}, "repo", "path or git URL of a repository to index (repeatable)")
		fs.BoolVar(&opts.Force, "force", false, "overwrite an existing /review-branch command")
		fs.Parse(flags)
		err = cli.Setup(ctx, firstArgOr(path, "."), os.Stdout, opts)

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
		// Flags pulled out by hand rather than by flag.Parse. Go's flag
		// package stops at the first non-flag argument, so the documented
		// spelling `eng clean . --yes` left --yes in Args() and the
		// command told the user to re-run with the flag they had just
		// passed. A path argument must not change what a flag means.
		path, flags := splitFlags(args)
		fs := flag.NewFlagSet("clean", flag.ExitOnError)
		yes := fs.Bool("yes", false, "delete without asking")
		fs.Parse(flags)
		err = cli.Clean(ctx, firstArgOr(path, "."), os.Stdout, *yes)

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
		_, flags := splitFlags(args)
		fs := flag.NewFlagSet("review", flag.ExitOnError)
		noLaunch := fs.Bool("no-launch", false, "print the instructions without starting Claude Code")
		fs.Parse(flags)
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

// splitFlags separates positional arguments from flags, in any order.
//
// Go's flag package stops parsing at the first non-flag argument, which
// makes `eng clean . --yes` and `eng clean --yes .` behave differently
// for a reason no user can see. Ordering is not a thing anyone should
// have to know about a CLI.
//
// valueFlags names the flags that take a separate argument. Without it
// every flag is assumed boolean, and `--rules ./engineering` is split
// into a flag and a positional path — so `eng setup ~/os --rules
// ./engineering` would create the workspace at ./engineering and attach
// nothing, which is wrong in a way that looks like it worked.
func splitFlags(args []string, valueFlags ...string) (positional, flags []string) {
	takesValue := make(map[string]bool, len(valueFlags))
	for _, f := range valueFlags {
		takesValue[f] = true
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)

		// `--rules=x` carries its own value; `--rules x` claims the next
		// argument. Only the second form can steal a positional.
		name := strings.TrimLeft(a, "-")
		inline := strings.ContainsRune(name, '=')
		name, _, _ = strings.Cut(name, "=")
		if !inline && takesValue[name] && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return positional, flags
}

// repeatable collects a flag given more than once, so `--rules a --rules
// b` names two repositories rather than the second silently replacing
// the first. A workspace is usually built from several repositories, and
// a flag that keeps only the last one loses them without saying so.
type repeatable struct{ into *[]string }

func (r repeatable) String() string {
	if r.into == nil {
		return ""
	}
	return strings.Join(*r.into, ", ")
}

func (r repeatable) Set(v string) error {
	*r.into = append(*r.into, v)
	return nil
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
  setup [path] --rules <repo> [--repo <repo>]
                           do all of it: workspace, index, MCP, Claude Code
  doctor                   check this machine end to end, and say what to fix
  taxonomy auto            propose what this repository's directories mean, and ask
  review                   check the setup, then hand over to Claude Code

  --rules and --repo take a local path or a git URL, and may be repeated.
  Example:
    eng setup ~/engineering-os \
      --rules git@github.com:truelogics/engineering.git \
      --repo ~/code/your-application

Everyday:
  init [path]              create a workspace here (setup does this for you)
  index [path]             index every repository in the workspace
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
  taxonomy auto            propose mappings from what this repository contains
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
	positional, flagArgs := splitFlags(args)
	if len(positional) == 0 {
		return cli.Taxonomy(ctx, ".", os.Stdout)
	}
	switch positional[0] {
	case "validate":
		return cli.TaxonomyValidate(ctx, ".", os.Stdout)
	case "auto":
		fs := flag.NewFlagSet("taxonomy auto", flag.ExitOnError)
		update := fs.Bool("update", false, "propose an update to an existing taxonomy")
		yes := fs.Bool("yes", false, "apply the proposal without the interactive prompt")
		fs.Parse(flagArgs)
		return cli.TaxonomyAuto(ctx, ".", os.Stdin, os.Stdout, *update, *yes)
	case "suggest":
		// The name RFC-0008 used for what `auto` does. Aliased rather than
		// refused: this used to error on the grounds that a guess presented
		// as a suggestion gets accepted without being decided, and `auto`
		// answers that objection by never deciding — it shows its evidence,
		// omits what it cannot identify, and stops at the prompt.
		fmt.Fprintln(os.Stderr, "eng taxonomy suggest: use `eng taxonomy auto`, which proposes and then asks.")
		return runTaxonomy(ctx, append([]string{"auto"}, flagArgs...))
	default:
		return fmt.Errorf("eng taxonomy: unknown command %q — try `eng taxonomy`, `eng taxonomy auto` or `eng taxonomy validate`", positional[0])
	}
}
