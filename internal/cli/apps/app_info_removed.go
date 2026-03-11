package apps

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/peterbourgon/ff/v3/ffcli"

	"github.com/rudrankriyam/App-Store-Connect-CLI/internal/cli/shared"
)

// RemovedAppInfoCommand catches legacy app-info invocations and points callers to apps info.
func RemovedAppInfoCommand() *ffcli.Command {
	fs := flag.NewFlagSet("app-info", flag.ExitOnError)

	return &ffcli.Command{
		Name:       "app-info",
		ShortUsage: "asc apps info <subcommand> [flags]",
		ShortHelp:  "DEPRECATED: use `asc apps info ...`.",
		LongHelp: `Use the new app-scoped info commands instead:

- ` + "`asc apps info list --app \"APP_ID\"`" + ` to inspect app info records
- ` + "`asc apps info view --app \"APP_ID\"`" + ` to read metadata/localizations
- ` + "`asc apps info edit --app \"APP_ID\" --locale \"en-US\" --whats-new \"Bug fixes\"`" + ` to update metadata

The old root-level ` + "`asc app-info ...`" + ` path was removed.`,
		FlagSet:   fs,
		UsageFunc: shared.DeprecatedUsageFunc,
		Exec: func(ctx context.Context, args []string) error {
			fmt.Fprintln(os.Stderr, "Error: `asc app-info` was removed.")
			fmt.Fprintf(os.Stderr, "Use `%s` instead.\n", removedAppInfoSuggestion(args))
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "New commands:")
			fmt.Fprintln(os.Stderr, `  asc apps info list --app "APP_ID"`)
			fmt.Fprintln(os.Stderr, `  asc apps info view --app "APP_ID"`)
			fmt.Fprintln(os.Stderr, `  asc apps info edit --app "APP_ID" --locale "en-US" --whats-new "Bug fixes"`)
			return flag.ErrHelp
		},
	}
}

// RemovedAppInfosCommand catches legacy app-infos invocations and points callers to apps info.
func RemovedAppInfosCommand() *ffcli.Command {
	fs := flag.NewFlagSet("app-infos", flag.ExitOnError)

	return &ffcli.Command{
		Name:       "app-infos",
		ShortUsage: "asc apps info list [flags]",
		ShortHelp:  "DEPRECATED: use `asc apps info list`.",
		LongHelp: `Use ` + "`asc apps info list --app \"APP_ID\"`" + ` to inspect app info records for an app.

The old root-level ` + "`asc app-infos ...`" + ` path was removed.`,
		FlagSet:   fs,
		UsageFunc: shared.DeprecatedUsageFunc,
		Exec: func(ctx context.Context, args []string) error {
			fmt.Fprintln(os.Stderr, "Error: `asc app-infos` was removed.")
			fmt.Fprintln(os.Stderr, `Use `+"`asc apps info list --app \"APP_ID\"`"+` instead.`)
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "New commands:")
			fmt.Fprintln(os.Stderr, `  asc apps info list --app "APP_ID"`)
			fmt.Fprintln(os.Stderr, `  asc apps info view --app "APP_ID"`)
			fmt.Fprintln(os.Stderr, `  asc apps info edit --app "APP_ID" --locale "en-US" --whats-new "Bug fixes"`)
			return flag.ErrHelp
		},
	}
}

func removedAppInfoSuggestion(args []string) string {
	if len(args) == 0 {
		return "asc apps info"
	}

	switch strings.TrimSpace(args[0]) {
	case "list":
		return `asc apps info list --app "APP_ID"`
	case "get", "view":
		return `asc apps info view --app "APP_ID"`
	case "set", "edit":
		return `asc apps info edit --app "APP_ID" --locale "en-US" --whats-new "Bug fixes"`
	case "relationships":
		if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
			return fmt.Sprintf(`asc apps info relationships %s --app "APP_ID"`, strings.TrimSpace(args[1]))
		}
		return `asc apps info relationships primary-category --app "APP_ID"`
	case "territory-age-ratings":
		return `asc apps info territory-age-ratings list --app "APP_ID"`
	default:
		return "asc apps info"
	}
}
