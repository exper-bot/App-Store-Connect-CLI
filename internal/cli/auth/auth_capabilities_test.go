package auth

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rudrankriyam/App-Store-Connect-CLI/internal/asc"
	"github.com/rudrankriyam/App-Store-Connect-CLI/internal/cli/shared"
)

func TestAuthCapabilitiesCommandFlagValidation(t *testing.T) {
	t.Run("unsupported output", func(t *testing.T) {
		cmd := AuthCapabilitiesCommand()
		if err := cmd.FlagSet.Parse([]string{"--output", "yaml"}); err != nil {
			t.Fatalf("Parse() error: %v", err)
		}

		_, stderr := captureAuthOutput(t, func() {
			err := cmd.Exec(context.Background(), []string{})
			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("expected flag.ErrHelp, got %v", err)
			}
		})
		if !strings.Contains(stderr, "unsupported format") {
			t.Fatalf("expected unsupported format error, got %q", stderr)
		}
	})

	t.Run("pretty requires json output", func(t *testing.T) {
		cmd := AuthCapabilitiesCommand()
		if err := cmd.FlagSet.Parse([]string{"--output", "table", "--pretty"}); err != nil {
			t.Fatalf("Parse() error: %v", err)
		}

		_, stderr := captureAuthOutput(t, func() {
			err := cmd.Exec(context.Background(), []string{})
			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("expected flag.ErrHelp, got %v", err)
			}
		})
		if !strings.Contains(stderr, "--pretty is only valid with JSON output") {
			t.Fatalf("expected pretty/json error, got %q", stderr)
		}
	})
}

func TestAuthCommandIncludesCapabilitiesSubcommand(t *testing.T) {
	cmd := AuthCommand()
	found := false
	for _, sub := range cmd.Subcommands {
		if sub != nil && sub.Name == "capabilities" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected auth command to include capabilities subcommand")
	}
}

func TestDefaultAuthCapabilitiesOutputFormat(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		t.Setenv("ASC_DEFAULT_OUTPUT", "json")
		shared.ResetDefaultOutputFormat()
		t.Cleanup(shared.ResetDefaultOutputFormat)

		if got := defaultAuthCapabilitiesOutputFormat(); got != "json" {
			t.Fatalf("defaultAuthCapabilitiesOutputFormat() = %q, want %q", got, "json")
		}
	})

	t.Run("markdown", func(t *testing.T) {
		t.Setenv("ASC_DEFAULT_OUTPUT", "markdown")
		shared.ResetDefaultOutputFormat()
		t.Cleanup(shared.ResetDefaultOutputFormat)

		if got := defaultAuthCapabilitiesOutputFormat(); got != "markdown" {
			t.Fatalf("defaultAuthCapabilitiesOutputFormat() = %q, want %q", got, "markdown")
		}
	})
}

func TestAuthCapabilitiesCommandHelpClarifiesEffectiveAccess(t *testing.T) {
	cmd := AuthCapabilitiesCommand()

	if got := cmd.ShortHelp; got != "Probe effective App Store Connect access for the current credential." {
		t.Fatalf("ShortHelp = %q", got)
	}
	if !strings.Contains(cmd.LongHelp, "Reports effective API access only.") {
		t.Fatalf("LongHelp = %q, want effective-access clarification", cmd.LongHelp)
	}
	if !strings.Contains(cmd.LongHelp, "does not infer or guarantee the exact") {
		t.Fatalf("LongHelp = %q, want role-inference clarification", cmd.LongHelp)
	}
}

func TestSummarizeAuthCapabilities(t *testing.T) {
	red := summarizeAuthCapabilities([]authCapabilityCheck{
		{Name: "apps", Status: "available"},
		{Name: "analytics", Status: "inconclusive", Message: "analytics probe failed"},
	})
	if red.Health != "red" || red.InconclusiveCount != 1 || red.NextAction != "analytics probe failed" {
		t.Fatalf("unexpected red summary: %+v", red)
	}

	yellow := summarizeAuthCapabilities([]authCapabilityCheck{
		{Name: "apps", Status: "available"},
		{Name: "sales", Status: "unavailable", Message: "sales unavailable"},
	})
	if yellow.Health != "yellow" || yellow.UnavailableCount != 1 || yellow.NextAction != "sales unavailable" {
		t.Fatalf("unexpected yellow summary: %+v", yellow)
	}

	green := summarizeAuthCapabilities([]authCapabilityCheck{
		{Name: "apps", Status: "available"},
		{Name: "builds", Status: "skipped", Message: "provide --app"},
	})
	if green.Health != "green" || green.SkippedCount != 1 || green.NextAction != "provide --app" {
		t.Fatalf("unexpected green summary: %+v", green)
	}
}

func TestAuthCapabilitiesCommandJSONOutput(t *testing.T) {
	prevCollector := authCapabilitiesCollector
	authCapabilitiesCollector = func(context.Context, string, string) (*authCapabilitiesResponse, error) {
		return &authCapabilitiesResponse{
			Summary: authCapabilitiesSummary{
				Health:         "green",
				NextAction:     "No action needed.",
				AvailableCount: 1,
			},
			Inputs: authCapabilitiesInputs{
				AppID:        "123456789",
				VendorNumber: "98765432",
			},
			Capabilities: []authCapabilityCheck{
				{Name: "apps", Scope: "account", Status: "available", Message: "can list apps"},
			},
			GeneratedAt: "2026-03-12T00:00:00Z",
		}, nil
	}
	t.Cleanup(func() {
		authCapabilitiesCollector = prevCollector
	})

	cmd := AuthCapabilitiesCommand()
	if err := cmd.FlagSet.Parse([]string{"--output", "json"}); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	stdout, _ := captureAuthOutput(t, func() {
		if err := cmd.Exec(context.Background(), []string{}); err != nil {
			t.Fatalf("Exec() error: %v", err)
		}
	})

	var payload authCapabilitiesResponse
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if payload.Summary.Health != "green" || len(payload.Capabilities) != 1 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestAuthCapabilitiesCommand_TrimsResolvedInputs(t *testing.T) {
	prevCollector := authCapabilitiesCollector
	var gotAppID string
	var gotVendor string
	authCapabilitiesCollector = func(_ context.Context, appID, vendorNumber string) (*authCapabilitiesResponse, error) {
		gotAppID = appID
		gotVendor = vendorNumber
		return &authCapabilitiesResponse{
			Summary: authCapabilitiesSummary{
				Health:         "green",
				NextAction:     "No action needed.",
				AvailableCount: 1,
			},
			Capabilities: []authCapabilityCheck{
				{Name: "apps", Scope: "account", Status: "available", Message: "can list apps"},
			},
			GeneratedAt: "2026-03-12T00:00:00Z",
		}, nil
	}
	t.Cleanup(func() {
		authCapabilitiesCollector = prevCollector
	})

	cmd := AuthCapabilitiesCommand()
	if err := cmd.FlagSet.Parse([]string{"--app", " 123456789 ", "--vendor", " 98765432 ", "--output", "json"}); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	_, _ = captureAuthOutput(t, func() {
		if err := cmd.Exec(context.Background(), []string{}); err != nil {
			t.Fatalf("Exec() error: %v", err)
		}
	})

	if gotAppID != "123456789" {
		t.Fatalf("collector appID = %q, want %q", gotAppID, "123456789")
	}
	if gotVendor != "98765432" {
		t.Fatalf("collector vendorNumber = %q, want %q", gotVendor, "98765432")
	}
}

func TestRenderAuthCapabilitiesMarkdown(t *testing.T) {
	stdout, _ := captureAuthOutput(t, func() {
		renderAuthCapabilities(&authCapabilitiesResponse{
			Summary: authCapabilitiesSummary{
				Health:         "green",
				NextAction:     "No action needed.",
				AvailableCount: 1,
			},
			Capabilities: []authCapabilityCheck{
				{Name: "apps", Scope: "account", Status: "available", Message: "can list apps"},
			},
			GeneratedAt: "2026-03-12T00:00:00Z",
		}, true)
	})
	if !strings.Contains(stdout, "### Summary") || !strings.Contains(stdout, "### Capabilities") {
		t.Fatalf("expected markdown sections, got %q", stdout)
	}
}

func TestCollectAuthCapabilities_AllAvailable(t *testing.T) {
	prevClientFn := authCapabilitiesClientFn
	prevNow := authCapabilitiesNow
	authCapabilitiesClientFn = func() (authCapabilitiesClient, error) {
		return &authCapabilitiesClientStub{
			salesDownload:   &asc.ReportDownload{Body: io.NopCloser(strings.NewReader("sales"))},
			financeDownload: &asc.ReportDownload{Body: io.NopCloser(strings.NewReader("finance"))},
		}, nil
	}
	authCapabilitiesNow = func() time.Time { return time.Date(2026, time.March, 12, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() {
		authCapabilitiesClientFn = prevClientFn
		authCapabilitiesNow = prevNow
	})

	resp, err := collectAuthCapabilities(context.Background(), "123456789", "98765432")
	if err != nil {
		t.Fatalf("collectAuthCapabilities() error: %v", err)
	}
	if resp.Summary.Health != "green" || resp.Summary.AvailableCount != 7 {
		t.Fatalf("unexpected summary: %+v", resp.Summary)
	}
}

func TestCollectAuthCapabilities_SkipsMissingScopes(t *testing.T) {
	prevClientFn := authCapabilitiesClientFn
	authCapabilitiesClientFn = func() (authCapabilitiesClient, error) {
		return &authCapabilitiesClientStub{}, nil
	}
	t.Cleanup(func() {
		authCapabilitiesClientFn = prevClientFn
	})

	resp, err := collectAuthCapabilities(context.Background(), "", "")
	if err != nil {
		t.Fatalf("collectAuthCapabilities() error: %v", err)
	}
	if resp.Summary.AvailableCount != 1 || resp.Summary.SkippedCount != 6 {
		t.Fatalf("unexpected summary: %+v", resp.Summary)
	}
}

func TestCollectAuthCapabilities_MapsDeniedAndUnauthorized(t *testing.T) {
	prevClientFn := authCapabilitiesClientFn
	authCapabilitiesClientFn = func() (authCapabilitiesClient, error) {
		return &authCapabilitiesClientStub{
			getAppsErr:    asc.ErrForbidden,
			getBuildsErr:  asc.ErrForbidden,
			getReviewsErr: asc.ErrUnauthorized,
		}, nil
	}
	t.Cleanup(func() {
		authCapabilitiesClientFn = prevClientFn
	})

	resp, err := collectAuthCapabilities(context.Background(), "123456789", "")
	if err != nil {
		t.Fatalf("collectAuthCapabilities() error: %v", err)
	}
	if resp.Capabilities[0].Status != "unavailable" {
		t.Fatalf("apps status = %q, want unavailable", resp.Capabilities[0].Status)
	}
	if resp.Capabilities[1].Status != "unavailable" {
		t.Fatalf("builds status = %q, want unavailable", resp.Capabilities[1].Status)
	}
	if resp.Capabilities[2].Status != "inconclusive" || !strings.Contains(resp.Capabilities[2].Message, "unauthorized or expired") {
		t.Fatalf("unexpected reviews check: %+v", resp.Capabilities[2])
	}
}

func TestCollectAuthCapabilities_SalesReportLagMakesSummaryRed(t *testing.T) {
	prevClientFn := authCapabilitiesClientFn
	prevNow := authCapabilitiesNow
	authCapabilitiesClientFn = func() (authCapabilitiesClient, error) {
		return &authCapabilitiesClientStub{
			salesErrorsByDate: map[string]error{
				"2026-03-11": asc.ErrNotFound,
				"2026-03-10": asc.ErrNotFound,
				"2026-03-09": asc.ErrNotFound,
				"2026-03-08": asc.ErrNotFound,
				"2026-03-07": asc.ErrNotFound,
				"2026-03-06": asc.ErrNotFound,
				"2026-03-05": asc.ErrNotFound,
			},
			financeDownload: &asc.ReportDownload{Body: io.NopCloser(strings.NewReader("finance"))},
		}, nil
	}
	authCapabilitiesNow = func() time.Time { return time.Date(2026, time.March, 12, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() {
		authCapabilitiesClientFn = prevClientFn
		authCapabilitiesNow = prevNow
	})

	resp, err := collectAuthCapabilities(context.Background(), "", "98765432")
	if err != nil {
		t.Fatalf("collectAuthCapabilities() error: %v", err)
	}
	if resp.Summary.Health != "red" {
		t.Fatalf("summary health = %q, want red (%+v)", resp.Summary.Health, resp.Summary)
	}
	if resp.Capabilities[5].Status != "inconclusive" {
		t.Fatalf("sales status = %q, want inconclusive (%+v)", resp.Capabilities[5].Status, resp.Capabilities[5])
	}
}

func TestAuthCapabilityCheckFromErrorNotFound(t *testing.T) {
	check := authCapabilityCheckFromError(
		"analytics",
		"app",
		asc.ErrNotFound,
		"ok",
		"denied",
		"analytics probe failed",
	)
	if check.Status != "inconclusive" || !strings.Contains(check.Message, "not found or is not visible") {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestCollectAuthCapabilities_UsesExpectedVendorReportDates(t *testing.T) {
	stub := &authCapabilitiesClientStub{
		salesDownload:   &asc.ReportDownload{Body: io.NopCloser(strings.NewReader("sales"))},
		financeDownload: &asc.ReportDownload{Body: io.NopCloser(strings.NewReader("finance"))},
	}
	prevClientFn := authCapabilitiesClientFn
	prevNow := authCapabilitiesNow
	authCapabilitiesClientFn = func() (authCapabilitiesClient, error) {
		return stub, nil
	}
	authCapabilitiesNow = func() time.Time { return time.Date(2026, time.March, 12, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() {
		authCapabilitiesClientFn = prevClientFn
		authCapabilitiesNow = prevNow
	})

	if _, err := collectAuthCapabilities(context.Background(), "", "98765432"); err != nil {
		t.Fatalf("collectAuthCapabilities() error: %v", err)
	}
	if stub.lastSalesParams.ReportDate != "2026-03-11" {
		t.Fatalf("sales report date = %q, want %q", stub.lastSalesParams.ReportDate, "2026-03-11")
	}
	if stub.lastFinanceParams.ReportDate != "2026-02" {
		t.Fatalf("finance report date = %q, want %q", stub.lastFinanceParams.ReportDate, "2026-02")
	}
}

func TestAuthSalesCapabilityCheck_TriesRecentDaysBeforeGivingUp(t *testing.T) {
	prevNow := authCapabilitiesNow
	authCapabilitiesNow = func() time.Time { return time.Date(2026, time.March, 12, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() {
		authCapabilitiesNow = prevNow
	})

	stub := &authCapabilitiesClientStub{
		salesErrorsByDate: map[string]error{
			"2026-03-11": asc.ErrNotFound,
			"2026-03-10": nil,
		},
		salesDownload: &asc.ReportDownload{Body: io.NopCloser(strings.NewReader("sales"))},
	}

	check := authSalesCapabilityCheck(context.Background(), stub, "98765432")
	if check.Status != "available" {
		t.Fatalf("status = %q, want available (%+v)", check.Status, check)
	}
	if got, want := stub.salesReportDates, []string{"2026-03-11", "2026-03-10"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("sales report dates = %v, want %v", got, want)
	}
}

func TestAuthSalesCapabilityCheck_KeepsRecentNotFoundsInconclusive(t *testing.T) {
	prevNow := authCapabilitiesNow
	authCapabilitiesNow = func() time.Time { return time.Date(2026, time.March, 12, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() {
		authCapabilitiesNow = prevNow
	})

	stub := &authCapabilitiesClientStub{
		salesErrorsByDate: map[string]error{
			"2026-03-11": asc.ErrNotFound,
			"2026-03-10": asc.ErrNotFound,
			"2026-03-09": asc.ErrNotFound,
			"2026-03-08": asc.ErrNotFound,
			"2026-03-07": asc.ErrNotFound,
			"2026-03-06": asc.ErrNotFound,
			"2026-03-05": asc.ErrNotFound,
		},
	}

	check := authSalesCapabilityCheck(context.Background(), stub, "98765432")
	if check.Status != "inconclusive" {
		t.Fatalf("status = %q, want inconclusive (%+v)", check.Status, check)
	}
	if !strings.Contains(check.Message, "sales reports may lag behind") {
		t.Fatalf("message = %q, want sales lag guidance", check.Message)
	}
	if got, want := stub.salesReportDates, []string{"2026-03-11", "2026-03-10", "2026-03-09", "2026-03-08", "2026-03-07", "2026-03-06", "2026-03-05"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] || got[4] != want[4] || got[5] != want[5] || got[6] != want[6] {
		t.Fatalf("sales report dates = %v, want %v", got, want)
	}
}

func TestAuthFinanceCapabilityCheck_TriesRecentMonthsBeforeGivingUp(t *testing.T) {
	prevNow := authCapabilitiesNow
	authCapabilitiesNow = func() time.Time { return time.Date(2026, time.March, 12, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() {
		authCapabilitiesNow = prevNow
	})

	stub := &authCapabilitiesClientStub{
		financeErrorsByDate: map[string]error{
			"2026-02": asc.ErrNotFound,
			"2026-01": nil,
		},
		financeDownload: &asc.ReportDownload{Body: io.NopCloser(strings.NewReader("finance"))},
	}

	check := authFinanceCapabilityCheck(context.Background(), stub, "98765432")
	if check.Status != "available" {
		t.Fatalf("status = %q, want available (%+v)", check.Status, check)
	}
	if got, want := stub.financeReportDates, []string{"2026-02", "2026-01"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("finance report dates = %v, want %v", got, want)
	}
}

func TestAuthFinanceCapabilityCheck_KeepsRecentNotFoundsInconclusive(t *testing.T) {
	prevNow := authCapabilitiesNow
	authCapabilitiesNow = func() time.Time { return time.Date(2026, time.March, 31, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() {
		authCapabilitiesNow = prevNow
	})

	stub := &authCapabilitiesClientStub{
		financeErrorsByDate: map[string]error{
			"2026-02": asc.ErrNotFound,
			"2026-01": asc.ErrNotFound,
			"2025-12": asc.ErrNotFound,
		},
	}

	check := authFinanceCapabilityCheck(context.Background(), stub, "98765432")
	if check.Status != "inconclusive" {
		t.Fatalf("status = %q, want inconclusive (%+v)", check.Status, check)
	}
	if !strings.Contains(check.Message, "Apple fiscal months") {
		t.Fatalf("message = %q, want Apple fiscal months guidance", check.Message)
	}
	if !strings.Contains(check.Message, "returned not found") {
		t.Fatalf("message = %q, want not found guidance", check.Message)
	}
	if got, want := stub.financeReportDates, []string{"2026-02", "2026-01", "2025-12"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("finance report dates = %v, want %v", got, want)
	}
}

func TestCollectAuthCapabilities_ProbesRunConcurrently(t *testing.T) {
	prevClientFn := authCapabilitiesClientFn
	stub := &authCapabilitiesConcurrentClientStub{delay: 25 * time.Millisecond}
	authCapabilitiesClientFn = func() (authCapabilitiesClient, error) {
		return stub, nil
	}
	t.Cleanup(func() {
		authCapabilitiesClientFn = prevClientFn
	})

	if _, err := collectAuthCapabilities(context.Background(), "123456789", "98765432"); err != nil {
		t.Fatalf("collectAuthCapabilities() error: %v", err)
	}
	if got := stub.MaxActive(); got < 2 {
		t.Fatalf("max active probes = %d, want at least 2", got)
	}
}

type authCapabilitiesClientStub struct {
	getAppsErr               error
	getBuildsErr             error
	getReviewsErr            error
	getSubscriptionGroupsErr error
	getAnalyticsErr          error
	getSalesErr              error
	getFinanceErr            error
	salesErrorsByDate        map[string]error
	financeErrorsByDate      map[string]error
	salesDownload            *asc.ReportDownload
	financeDownload          *asc.ReportDownload
	lastSalesParams          asc.SalesReportParams
	lastFinanceParams        asc.FinanceReportParams
	salesReportDates         []string
	financeReportDates       []string
}

func (s *authCapabilitiesClientStub) GetApps(context.Context, ...asc.AppsOption) (*asc.AppsResponse, error) {
	return &asc.AppsResponse{}, s.getAppsErr
}

func (s *authCapabilitiesClientStub) GetBuilds(context.Context, string, ...asc.BuildsOption) (*asc.BuildsResponse, error) {
	return &asc.BuildsResponse{}, s.getBuildsErr
}

func (s *authCapabilitiesClientStub) GetReviews(context.Context, string, ...asc.ReviewOption) (*asc.ReviewsResponse, error) {
	return &asc.ReviewsResponse{}, s.getReviewsErr
}

func (s *authCapabilitiesClientStub) GetSubscriptionGroups(context.Context, string, ...asc.SubscriptionGroupsOption) (*asc.SubscriptionGroupsResponse, error) {
	return &asc.SubscriptionGroupsResponse{}, s.getSubscriptionGroupsErr
}

func (s *authCapabilitiesClientStub) GetAnalyticsReportRequests(context.Context, string, ...asc.AnalyticsReportRequestsOption) (*asc.AnalyticsReportRequestsResponse, error) {
	return &asc.AnalyticsReportRequestsResponse{}, s.getAnalyticsErr
}

func (s *authCapabilitiesClientStub) GetSalesReport(_ context.Context, params asc.SalesReportParams) (*asc.ReportDownload, error) {
	s.lastSalesParams = params
	s.salesReportDates = append(s.salesReportDates, params.ReportDate)
	if s.salesDownload == nil {
		s.salesDownload = &asc.ReportDownload{Body: io.NopCloser(strings.NewReader(""))}
	}
	if err, ok := s.salesErrorsByDate[params.ReportDate]; ok {
		return s.salesDownload, err
	}
	return s.salesDownload, s.getSalesErr
}

func (s *authCapabilitiesClientStub) DownloadFinanceReport(_ context.Context, params asc.FinanceReportParams) (*asc.ReportDownload, error) {
	s.lastFinanceParams = params
	s.financeReportDates = append(s.financeReportDates, params.ReportDate)
	if s.financeDownload == nil {
		s.financeDownload = &asc.ReportDownload{Body: io.NopCloser(strings.NewReader(""))}
	}
	if err, ok := s.financeErrorsByDate[params.ReportDate]; ok {
		return s.financeDownload, err
	}
	return s.financeDownload, s.getFinanceErr
}

type authCapabilitiesConcurrentClientStub struct {
	mu        sync.Mutex
	active    int
	maxActive int
	delay     time.Duration
}

func (s *authCapabilitiesConcurrentClientStub) enter() {
	s.mu.Lock()
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.mu.Unlock()
	time.Sleep(s.delay)
}

func (s *authCapabilitiesConcurrentClientStub) exit() {
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
}

func (s *authCapabilitiesConcurrentClientStub) MaxActive() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxActive
}

func (s *authCapabilitiesConcurrentClientStub) GetApps(context.Context, ...asc.AppsOption) (*asc.AppsResponse, error) {
	s.enter()
	defer s.exit()
	return &asc.AppsResponse{}, nil
}

func (s *authCapabilitiesConcurrentClientStub) GetBuilds(context.Context, string, ...asc.BuildsOption) (*asc.BuildsResponse, error) {
	s.enter()
	defer s.exit()
	return &asc.BuildsResponse{}, nil
}

func (s *authCapabilitiesConcurrentClientStub) GetReviews(context.Context, string, ...asc.ReviewOption) (*asc.ReviewsResponse, error) {
	s.enter()
	defer s.exit()
	return &asc.ReviewsResponse{}, nil
}

func (s *authCapabilitiesConcurrentClientStub) GetSubscriptionGroups(context.Context, string, ...asc.SubscriptionGroupsOption) (*asc.SubscriptionGroupsResponse, error) {
	s.enter()
	defer s.exit()
	return &asc.SubscriptionGroupsResponse{}, nil
}

func (s *authCapabilitiesConcurrentClientStub) GetAnalyticsReportRequests(context.Context, string, ...asc.AnalyticsReportRequestsOption) (*asc.AnalyticsReportRequestsResponse, error) {
	s.enter()
	defer s.exit()
	return &asc.AnalyticsReportRequestsResponse{}, nil
}

func (s *authCapabilitiesConcurrentClientStub) GetSalesReport(context.Context, asc.SalesReportParams) (*asc.ReportDownload, error) {
	s.enter()
	defer s.exit()
	return &asc.ReportDownload{Body: io.NopCloser(strings.NewReader("sales"))}, nil
}

func (s *authCapabilitiesConcurrentClientStub) DownloadFinanceReport(context.Context, asc.FinanceReportParams) (*asc.ReportDownload, error) {
	s.enter()
	defer s.exit()
	return &asc.ReportDownload{Body: io.NopCloser(strings.NewReader("finance"))}, nil
}
