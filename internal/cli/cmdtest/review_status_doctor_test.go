package cmdtest

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewStatusAndDoctorValidationErrors(t *testing.T) {
	t.Setenv("ASC_APP_ID", "")

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "review status missing app",
			args:    []string{"review", "status"},
			wantErr: "--app is required",
		},
		{
			name:    "review doctor missing app",
			args:    []string{"review", "doctor"},
			wantErr: "--app is required",
		},
		{
			name:    "review status mutually exclusive version flags",
			args:    []string{"review", "status", "--app", "123456789", "--version", "1.2.3", "--version-id", "ver-1"},
			wantErr: "--version and --version-id are mutually exclusive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := RootCommand("1.2.3")
			root.FlagSet.SetOutput(io.Discard)

			stdout, stderr := captureOutput(t, func() {
				if err := root.Parse(test.args); err != nil {
					t.Fatalf("parse error: %v", err)
				}
				err := root.Run(context.Background())
				if !errors.Is(err, flag.ErrHelp) {
					t.Fatalf("expected ErrHelp, got %v", err)
				}
			})

			if stdout != "" {
				t.Fatalf("expected empty stdout, got %q", stdout)
			}
			if !strings.Contains(stderr, test.wantErr) {
				t.Fatalf("expected error %q, got %q", test.wantErr, stderr)
			}
		})
	}
}

func TestReviewStatusShowsCurrentReviewState(t *testing.T) {
	setupAuth(t)
	t.Setenv("ASC_CONFIG_PATH", filepath.Join(t.TempDir(), "nonexistent.json"))
	t.Setenv("ASC_APP_ID", "")

	originalTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
	})

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/apps/123456789/appStoreVersions":
			return statusJSONResponse(`{
				"data":[
					{
						"type":"appStoreVersions",
						"id":"ver-1",
						"attributes":{
							"platform":"IOS",
							"versionString":"1.2.3",
							"appVersionState":"WAITING_FOR_REVIEW",
							"createdDate":"2026-03-15T00:00:00Z"
						}
					}
				],
				"links":{"next":""}
			}`), nil
		case "/v1/appStoreVersions/ver-1/appStoreReviewDetail":
			return statusJSONResponse(`{
				"data":{
					"type":"appStoreReviewDetails",
					"id":"detail-1",
					"attributes":{"contactEmail":"dev@example.com"}
				}
			}`), nil
		case "/v1/apps/123456789/reviewSubmissions":
			if req.URL.Query().Get("filter[platform]") != "IOS" {
				t.Fatalf("expected platform filter IOS, got %q", req.URL.Query().Get("filter[platform]"))
			}
			return statusJSONResponse(`{
				"data":[
					{
						"type":"reviewSubmissions",
						"id":"review-sub-1",
						"attributes":{"state":"WAITING_FOR_REVIEW","platform":"IOS","submittedDate":"2026-03-15T01:00:00Z"}
					}
				],
				"links":{"next":""}
			}`), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			return nil, nil
		}
	})

	root := RootCommand("1.2.3")
	root.FlagSet.SetOutput(io.Discard)

	stdout, stderr := captureOutput(t, func() {
		if err := root.Parse([]string{"review", "status", "--app", "123456789"}); err != nil {
			t.Fatalf("parse error: %v", err)
		}
		if err := root.Run(context.Background()); err != nil {
			t.Fatalf("run error: %v", err)
		}
	})

	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("unmarshal output: %v\nstdout=%s", err, stdout)
	}

	if payload["reviewState"] != "WAITING_FOR_REVIEW" {
		t.Fatalf("expected reviewState WAITING_FOR_REVIEW, got %v", payload["reviewState"])
	}
	if payload["nextAction"] != "Wait for App Store review outcome." {
		t.Fatalf("expected wait-for-review next action, got %v", payload["nextAction"])
	}
	if payload["reviewDetailId"] != "detail-1" {
		t.Fatalf("expected reviewDetailId detail-1, got %v", payload["reviewDetailId"])
	}

	latestSubmission, ok := payload["latestSubmission"].(map[string]any)
	if !ok {
		t.Fatalf("expected latestSubmission object, got %T", payload["latestSubmission"])
	}
	if latestSubmission["id"] != "review-sub-1" {
		t.Fatalf("expected latest submission id review-sub-1, got %v", latestSubmission["id"])
	}
}

func TestReviewStatusAndDoctorRejectVersionIDFromDifferentApp(t *testing.T) {
	setupAuth(t)
	t.Setenv("ASC_CONFIG_PATH", filepath.Join(t.TempDir(), "nonexistent.json"))
	t.Setenv("ASC_APP_ID", "")

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "review status version/app mismatch",
			args: []string{"review", "status", "--app", "123456789", "--version-id", "ver-1"},
		},
		{
			name: "review doctor version/app mismatch",
			args: []string{"review", "doctor", "--app", "123456789", "--version-id", "ver-1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalTransport := http.DefaultTransport
			t.Cleanup(func() {
				http.DefaultTransport = originalTransport
			})

			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/v1/appStoreVersions/ver-1":
					if req.URL.Query().Get("include") != "app" {
						t.Fatalf("expected include=app, got %q", req.URL.Query().Get("include"))
					}
					return statusJSONResponse(`{
						"data":{
							"type":"appStoreVersions",
							"id":"ver-1",
							"attributes":{"platform":"IOS","versionString":"1.2.3"},
							"relationships":{"app":{"data":{"type":"apps","id":"999999999"}}}
						}
					}`), nil
				default:
					t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
					return nil, nil
				}
			})

			root := RootCommand("1.2.3")
			root.FlagSet.SetOutput(io.Discard)

			var runErr error
			stdout, stderr := captureOutput(t, func() {
				if err := root.Parse(test.args); err != nil {
					t.Fatalf("parse error: %v", err)
				}
				runErr = root.Run(context.Background())
			})

			if runErr == nil {
				t.Fatal("expected version/app mismatch error")
			}
			if errors.Is(runErr, flag.ErrHelp) {
				t.Fatalf("expected runtime validation error, got ErrHelp")
			}
			if !strings.Contains(runErr.Error(), `version "ver-1" belongs to app "999999999", not "123456789"`) {
				t.Fatalf("expected mismatch error, got %v", runErr)
			}
			if stdout != "" {
				t.Fatalf("expected empty stdout, got %q", stdout)
			}
			if stderr != "" {
				t.Fatalf("expected empty stderr, got %q", stderr)
			}
		})
	}
}
