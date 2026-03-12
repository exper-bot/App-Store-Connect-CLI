package auth

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/ff/v3/ffcli"

	"github.com/rudrankriyam/App-Store-Connect-CLI/internal/asc"
	"github.com/rudrankriyam/App-Store-Connect-CLI/internal/cli/shared"
)

var (
	authCapabilitiesCollector = collectAuthCapabilities
	authCapabilitiesNow       = func() time.Time { return time.Now().UTC() }
	authCapabilitiesClientFn  = func() (authCapabilitiesClient, error) {
		client, err := shared.GetASCClient()
		if err != nil {
			return nil, err
		}
		return client, nil
	}
)

type authCapabilitiesClient interface {
	GetApps(ctx context.Context, opts ...asc.AppsOption) (*asc.AppsResponse, error)
	GetBuilds(ctx context.Context, appID string, opts ...asc.BuildsOption) (*asc.BuildsResponse, error)
	GetReviews(ctx context.Context, appID string, opts ...asc.ReviewOption) (*asc.ReviewsResponse, error)
	GetSubscriptionGroups(ctx context.Context, appID string, opts ...asc.SubscriptionGroupsOption) (*asc.SubscriptionGroupsResponse, error)
	GetAnalyticsReportRequests(ctx context.Context, appID string, opts ...asc.AnalyticsReportRequestsOption) (*asc.AnalyticsReportRequestsResponse, error)
	GetSalesReport(ctx context.Context, params asc.SalesReportParams) (*asc.ReportDownload, error)
	DownloadFinanceReport(ctx context.Context, params asc.FinanceReportParams) (*asc.ReportDownload, error)
}

// AuthCapabilitiesCommand returns the auth capabilities subcommand.
func AuthCapabilitiesCommand() *ffcli.Command {
	fs := flag.NewFlagSet("auth capabilities", flag.ExitOnError)

	appID := fs.String("app", "", "Optional app ID for app-scoped probes (or ASC_APP_ID env)")
	vendorNumber := fs.String("vendor", "", "Optional vendor number for sales/finance probes")
	output := shared.BindOutputFlagsWithAllowed(fs, "output", defaultAuthCapabilitiesOutputFormat(), "Output format: table, json, markdown", "table", "json", "markdown")

	return &ffcli.Command{
		Name:       "capabilities",
		ShortUsage: "asc auth capabilities [flags]",
		ShortHelp:  "Probe which App Store Connect surfaces the current credential can access.",
		LongHelp: `Probe which App Store Connect surfaces the current credential can access.

Runs read-only checks against a small set of App Store Connect resources to show
which capabilities are available to the current credential.

Examples:
  asc auth capabilities
  asc auth capabilities --app "123456789"
  asc auth capabilities --app "123456789" --vendor "98765432"
  asc auth capabilities --output markdown`,
		FlagSet:   fs,
		UsageFunc: shared.DefaultUsageFunc,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) > 0 {
				return shared.UsageErrorf("unexpected argument(s): %s", strings.Join(args, " "))
			}

			normalizedOutput, err := shared.ValidateOutputFormatAllowed(*output.Output, *output.Pretty, "table", "json", "markdown")
			if err != nil {
				return shared.UsageError(err.Error())
			}

			resp, err := authCapabilitiesCollector(
				ctx,
				shared.ResolveAppID(*appID),
				shared.ResolveVendorNumber(*vendorNumber),
			)
			if err != nil {
				return fmt.Errorf("auth capabilities: %w", err)
			}

			return shared.PrintOutputWithRenderers(
				resp,
				normalizedOutput,
				*output.Pretty,
				func() error { renderAuthCapabilities(resp, false); return nil },
				func() error { renderAuthCapabilities(resp, true); return nil },
			)
		},
	}
}

type authCapabilitiesResponse struct {
	Summary      authCapabilitiesSummary `json:"summary"`
	Inputs       authCapabilitiesInputs  `json:"inputs"`
	Capabilities []authCapabilityCheck   `json:"capabilities"`
	GeneratedAt  string                  `json:"generatedAt"`
}

type authCapabilitiesSummary struct {
	Health            string `json:"health"`
	NextAction        string `json:"nextAction"`
	AvailableCount    int    `json:"availableCount"`
	UnavailableCount  int    `json:"unavailableCount"`
	InconclusiveCount int    `json:"inconclusiveCount"`
	SkippedCount      int    `json:"skippedCount"`
}

type authCapabilitiesInputs struct {
	AppID        string `json:"appId,omitempty"`
	VendorNumber string `json:"vendorNumber,omitempty"`
}

type authCapabilityCheck struct {
	Name    string `json:"name"`
	Scope   string `json:"scope"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func collectAuthCapabilities(ctx context.Context, appID, vendorNumber string) (*authCapabilitiesResponse, error) {
	client, err := authCapabilitiesClientFn()
	if err != nil {
		return nil, err
	}

	checks := []authCapabilityCheck{
		authAppsCapabilityCheck(ctx, client),
		authBuildsCapabilityCheck(ctx, client, appID),
		authReviewsCapabilityCheck(ctx, client, appID),
		authSubscriptionsCapabilityCheck(ctx, client, appID),
		authAnalyticsCapabilityCheck(ctx, client, appID),
		authSalesCapabilityCheck(ctx, client, vendorNumber),
		authFinanceCapabilityCheck(ctx, client, vendorNumber),
	}
	summary := summarizeAuthCapabilities(checks)

	return &authCapabilitiesResponse{
		Summary: summary,
		Inputs: authCapabilitiesInputs{
			AppID:        appID,
			VendorNumber: vendorNumber,
		},
		Capabilities: checks,
		GeneratedAt:  authCapabilitiesNow().Format(time.RFC3339),
	}, nil
}

func summarizeAuthCapabilities(checks []authCapabilityCheck) authCapabilitiesSummary {
	summary := authCapabilitiesSummary{
		Health:     "green",
		NextAction: "No action needed.",
	}

	for _, check := range checks {
		switch check.Status {
		case "available":
			summary.AvailableCount++
		case "unavailable":
			summary.UnavailableCount++
		case "inconclusive":
			summary.InconclusiveCount++
		case "skipped":
			summary.SkippedCount++
		}
	}

	if summary.InconclusiveCount > 0 {
		summary.Health = "red"
		summary.NextAction = firstAuthCapabilityMessageByStatus(checks, "inconclusive")
		return summary
	}
	if summary.UnavailableCount > 0 {
		summary.Health = "yellow"
		summary.NextAction = firstAuthCapabilityMessageByStatus(checks, "unavailable")
		return summary
	}
	if summary.SkippedCount > 0 {
		summary.NextAction = firstAuthCapabilityMessageByStatus(checks, "skipped")
	}

	return summary
}

func firstAuthCapabilityMessageByStatus(checks []authCapabilityCheck, status string) string {
	for _, check := range checks {
		if check.Status == status {
			return check.Message
		}
	}
	return ""
}

func renderAuthCapabilities(resp *authCapabilitiesResponse, markdown bool) {
	summaryRows := [][]string{
		{"health", resp.Summary.Health},
		{"nextAction", resp.Summary.NextAction},
		{"availableCount", strconv.Itoa(resp.Summary.AvailableCount)},
		{"unavailableCount", strconv.Itoa(resp.Summary.UnavailableCount)},
		{"inconclusiveCount", strconv.Itoa(resp.Summary.InconclusiveCount)},
		{"skippedCount", strconv.Itoa(resp.Summary.SkippedCount)},
		{"appId", shared.OrNA(resp.Inputs.AppID)},
		{"vendorNumber", shared.OrNA(resp.Inputs.VendorNumber)},
		{"generatedAt", resp.GeneratedAt},
	}
	shared.RenderSection("Summary", []string{"field", "value"}, summaryRows, markdown)

	checkRows := make([][]string, 0, len(resp.Capabilities))
	for _, check := range resp.Capabilities {
		checkRows = append(checkRows, []string{check.Name, check.Scope, check.Status, check.Message})
	}
	shared.RenderSection("Capabilities", []string{"capability", "scope", "status", "message"}, checkRows, markdown)
}

func defaultAuthCapabilitiesOutputFormat() string {
	switch shared.DefaultOutputFormat() {
	case "json":
		return "json"
	case "markdown", "md":
		return "markdown"
	default:
		return "table"
	}
}

func authAppsCapabilityCheck(parent context.Context, client authCapabilitiesClient) authCapabilityCheck {
	requestCtx, cancel := shared.ContextWithTimeout(parent)
	defer cancel()

	_, err := client.GetApps(requestCtx, asc.WithAppsLimit(1))
	return authCapabilityCheckFromError(
		"apps",
		"account",
		err,
		"can list apps",
		"credentials are valid but apps listing is unavailable",
		"apps probe failed",
	)
}

func authBuildsCapabilityCheck(parent context.Context, client authCapabilitiesClient, appID string) authCapabilityCheck {
	if strings.TrimSpace(appID) == "" {
		return authSkippedCapabilityCheck("builds", "app", "provide --app or ASC_APP_ID to probe builds access")
	}

	requestCtx, cancel := shared.ContextWithTimeout(parent)
	defer cancel()

	_, err := client.GetBuilds(requestCtx, appID, asc.WithBuildsLimit(1))
	return authCapabilityCheckFromError(
		"builds",
		"app",
		err,
		fmt.Sprintf("can list builds for app %s", appID),
		fmt.Sprintf("credentials are valid but build access is unavailable for app %s", appID),
		fmt.Sprintf("builds probe failed for app %s", appID),
	)
}

func authReviewsCapabilityCheck(parent context.Context, client authCapabilitiesClient, appID string) authCapabilityCheck {
	if strings.TrimSpace(appID) == "" {
		return authSkippedCapabilityCheck("reviews", "app", "provide --app or ASC_APP_ID to probe reviews access")
	}

	requestCtx, cancel := shared.ContextWithTimeout(parent)
	defer cancel()

	_, err := client.GetReviews(requestCtx, appID, asc.WithLimit(1))
	return authCapabilityCheckFromError(
		"reviews",
		"app",
		err,
		fmt.Sprintf("can list customer reviews for app %s", appID),
		fmt.Sprintf("credentials are valid but customer review access is unavailable for app %s", appID),
		fmt.Sprintf("reviews probe failed for app %s", appID),
	)
}

func authSubscriptionsCapabilityCheck(parent context.Context, client authCapabilitiesClient, appID string) authCapabilityCheck {
	if strings.TrimSpace(appID) == "" {
		return authSkippedCapabilityCheck("subscriptions", "app", "provide --app or ASC_APP_ID to probe subscriptions access")
	}

	requestCtx, cancel := shared.ContextWithTimeout(parent)
	defer cancel()

	_, err := client.GetSubscriptionGroups(requestCtx, appID, asc.WithSubscriptionGroupsLimit(1))
	return authCapabilityCheckFromError(
		"subscriptions",
		"app",
		err,
		fmt.Sprintf("can list subscription groups for app %s", appID),
		fmt.Sprintf("credentials are valid but subscription group access is unavailable for app %s", appID),
		fmt.Sprintf("subscriptions probe failed for app %s", appID),
	)
}

func authAnalyticsCapabilityCheck(parent context.Context, client authCapabilitiesClient, appID string) authCapabilityCheck {
	if strings.TrimSpace(appID) == "" {
		return authSkippedCapabilityCheck("analytics", "app", "provide --app or ASC_APP_ID to probe analytics access")
	}

	requestCtx, cancel := shared.ContextWithTimeout(parent)
	defer cancel()

	_, err := client.GetAnalyticsReportRequests(requestCtx, appID, asc.WithAnalyticsReportRequestsLimit(1))
	return authCapabilityCheckFromError(
		"analytics",
		"app",
		err,
		fmt.Sprintf("can list analytics report requests for app %s", appID),
		fmt.Sprintf("credentials are valid but analytics access is unavailable for app %s", appID),
		fmt.Sprintf("analytics probe failed for app %s", appID),
	)
}

func authSalesCapabilityCheck(parent context.Context, client authCapabilitiesClient, vendorNumber string) authCapabilityCheck {
	if strings.TrimSpace(vendorNumber) == "" {
		return authSkippedCapabilityCheck("sales", "vendor", "provide --vendor or ASC_VENDOR_NUMBER to probe sales access")
	}

	successMessage := fmt.Sprintf("can request sales reports for vendor %s", vendorNumber)
	unavailableMessage := fmt.Sprintf("credentials are valid but sales report access is unavailable for vendor %s", vendorNumber)
	inconclusivePrefix := fmt.Sprintf("sales report probe failed for vendor %s", vendorNumber)

	var unavailableDates []string
	for _, reportDate := range authCapabilitiesSalesReportDates() {
		requestCtx, cancel := shared.ContextWithTimeout(parent)
		download, err := client.GetSalesReport(requestCtx, asc.SalesReportParams{
			VendorNumber:  vendorNumber,
			ReportType:    asc.SalesReportTypeSales,
			ReportSubType: asc.SalesReportSubTypeSummary,
			Frequency:     asc.SalesReportFrequencyDaily,
			ReportDate:    reportDate,
			Version:       asc.SalesReportVersion1_0,
		})
		cancel()
		if err == nil {
			closeReportDownload(download)
			return authCapabilityCheck{
				Name:    "sales",
				Scope:   "vendor",
				Status:  "available",
				Message: successMessage,
			}
		}
		if errors.Is(err, asc.ErrNotFound) || asc.IsNotFound(err) {
			unavailableDates = append(unavailableDates, reportDate)
			continue
		}
		return authCapabilityCheckFromError(
			"sales",
			"vendor",
			err,
			successMessage,
			unavailableMessage,
			inconclusivePrefix,
		)
	}

	return authSkippedCapabilityCheck(
		"sales",
		"vendor",
		fmt.Sprintf(
			"could not verify sales access for vendor %s because no recent daily sales reports were available for %s; sales reports may lag behind the requested date",
			vendorNumber,
			strings.Join(unavailableDates, ", "),
		),
	)
}

func authFinanceCapabilityCheck(parent context.Context, client authCapabilitiesClient, vendorNumber string) authCapabilityCheck {
	if strings.TrimSpace(vendorNumber) == "" {
		return authSkippedCapabilityCheck("finance", "vendor", "provide --vendor or ASC_VENDOR_NUMBER to probe finance access")
	}

	successMessage := fmt.Sprintf("can request finance reports for vendor %s", vendorNumber)
	unavailableMessage := fmt.Sprintf("credentials are valid but finance report access is unavailable for vendor %s", vendorNumber)
	inconclusivePrefix := fmt.Sprintf("finance report probe failed for vendor %s", vendorNumber)

	var unavailableDates []string
	for _, reportDate := range authCapabilitiesFinanceReportDates() {
		requestCtx, cancel := shared.ContextWithTimeout(parent)
		download, err := client.DownloadFinanceReport(requestCtx, asc.FinanceReportParams{
			VendorNumber: vendorNumber,
			ReportType:   asc.FinanceReportTypeFinancial,
			RegionCode:   "ZZ",
			ReportDate:   reportDate,
		})
		cancel()
		if err == nil {
			closeReportDownload(download)
			return authCapabilityCheck{
				Name:    "finance",
				Scope:   "vendor",
				Status:  "available",
				Message: successMessage,
			}
		}
		if errors.Is(err, asc.ErrNotFound) || asc.IsNotFound(err) {
			unavailableDates = append(unavailableDates, reportDate)
			continue
		}
		return authCapabilityCheckFromError(
			"finance",
			"vendor",
			err,
			successMessage,
			unavailableMessage,
			inconclusivePrefix,
		)
	}

	return authSkippedCapabilityCheck(
		"finance",
		"vendor",
		fmt.Sprintf(
			"could not verify finance access for vendor %s because no recent finance reports were available for %s; finance reports use Apple fiscal months",
			vendorNumber,
			strings.Join(unavailableDates, ", "),
		),
	)
}

func authCapabilityCheckFromError(name, scope string, err error, successMessage, unavailableMessage, inconclusivePrefix string) authCapabilityCheck {
	switch {
	case err == nil:
		return authCapabilityCheck{
			Name:    name,
			Scope:   scope,
			Status:  "available",
			Message: successMessage,
		}
	case errors.Is(err, asc.ErrForbidden):
		return authCapabilityCheck{
			Name:    name,
			Scope:   scope,
			Status:  "unavailable",
			Message: unavailableMessage,
		}
	case errors.Is(err, asc.ErrUnauthorized):
		return authCapabilityCheck{
			Name:    name,
			Scope:   scope,
			Status:  "inconclusive",
			Message: fmt.Sprintf("%s: credentials are unauthorized or expired", inconclusivePrefix),
		}
	case errors.Is(err, asc.ErrNotFound) || asc.IsNotFound(err):
		return authCapabilityCheck{
			Name:    name,
			Scope:   scope,
			Status:  "inconclusive",
			Message: fmt.Sprintf("%s: resource was not found or is not visible", inconclusivePrefix),
		}
	default:
		return authCapabilityCheck{
			Name:    name,
			Scope:   scope,
			Status:  "inconclusive",
			Message: fmt.Sprintf("%s: %v", inconclusivePrefix, err),
		}
	}
}

func authSkippedCapabilityCheck(name, scope, message string) authCapabilityCheck {
	return authCapabilityCheck{
		Name:    name,
		Scope:   scope,
		Status:  "skipped",
		Message: message,
	}
}

func closeReportDownload(download *asc.ReportDownload) {
	if download == nil || download.Body == nil {
		return
	}
	_ = download.Body.Close()
}

func authCapabilitiesSalesReportDates() []string {
	now := authCapabilitiesNow()
	dates := make([]string, 0, 7)
	for daysAgo := 1; daysAgo <= 7; daysAgo++ {
		dates = append(dates, now.AddDate(0, 0, -daysAgo).Format("2006-01-02"))
	}
	return dates
}

func authCapabilitiesFinanceReportDates() []string {
	now := authCapabilitiesNow()
	// Anchor to the first of the month so stepping backwards never rolls forward
	// into the current month near month end (for example Mar 31 -> Mar instead of Feb).
	anchor := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	dates := make([]string, 0, 3)
	for monthsAgo := 1; monthsAgo <= 3; monthsAgo++ {
		dates = append(dates, anchor.AddDate(0, -monthsAgo, 0).Format("2006-01"))
	}
	return dates
}
