package metadata

import (
	"context"
	"flag"

	"github.com/peterbourgon/ff/v3/ffcli"

	"github.com/rudrankriyam/App-Store-Connect-CLI/internal/cli/shared"
)

// MetadataApplyCommand returns the canonical apply alias for metadata push.
func MetadataApplyCommand() *ffcli.Command {
	fs := flag.NewFlagSet("metadata apply", flag.ExitOnError)

	appID := fs.String("app", "", "App Store Connect app ID (or ASC_APP_ID env)")
	appInfoID := fs.String("app-info", "", "App Info ID (optional override)")
	version := fs.String("version", "", "App version string (for example 1.2.3)")
	platform := fs.String("platform", "", "Optional platform: IOS, MAC_OS, TV_OS, or VISION_OS")
	dir := fs.String("dir", "", "Metadata root directory (required)")
	include := fs.String("include", includeLocalizations, "Included metadata scopes (comma-separated)")
	dryRun := fs.Bool("dry-run", false, "Preview changes without mutating App Store Connect")
	allowDeletes := fs.Bool("allow-deletes", false, "Allow destructive delete operations when applying changes (disables default locale fallback for missing locales)")
	confirm := fs.Bool("confirm", false, "Confirm destructive operations (required with --allow-deletes)")
	output := shared.BindOutputFlags(fs)

	return &ffcli.Command{
		Name:       "apply",
		ShortUsage: "asc metadata apply --app \"APP_ID\" --version \"1.2.3\" --dir \"./metadata\" [--app-info \"APP_INFO_ID\"] [--dry-run]",
		ShortHelp:  "Apply metadata changes from canonical files.",
		LongHelp: `Apply metadata changes from canonical files.

Examples:
  asc metadata apply --app "APP_ID" --version "1.2.3" --dir "./metadata" --dry-run
  asc metadata apply --app "APP_ID" --version "1.2.3" --platform IOS --dir "./metadata" --dry-run
  asc metadata apply --app "APP_ID" --app-info "APP_INFO_ID" --version "1.2.3" --platform IOS --dir "./metadata" --dry-run
  asc metadata apply --app "APP_ID" --version "1.2.3" --dir "./metadata"
  asc metadata apply --app "APP_ID" --version "1.2.3" --dir "./metadata" --allow-deletes --confirm

Notes:
  - default.json fallback is applied only when --allow-deletes is not set.
  - with --allow-deletes, remote locales missing locally are planned as deletes.
  - omitted fields are treated as no-op; they do not imply deletion.`,
		FlagSet:   fs,
		UsageFunc: shared.DefaultUsageFunc,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) > 0 {
				return shared.UsageError("metadata apply does not accept positional arguments")
			}
			result, err := ExecutePush(ctx, PushExecutionOptions{
				AppID:        *appID,
				AppInfoID:    *appInfoID,
				Version:      *version,
				Platform:     *platform,
				Dir:          *dir,
				Include:      *include,
				DryRun:       *dryRun,
				AllowDeletes: *allowDeletes,
				Confirm:      *confirm,
			})
			if err != nil {
				return err
			}
			return shared.PrintOutputWithRenderers(
				result,
				*output.Output,
				*output.Pretty,
				func() error { return printPushPlanTable(result) },
				func() error { return printPushPlanMarkdown(result) },
			)
		},
	}
}
