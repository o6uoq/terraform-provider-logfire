// Copyright Pydantic, Inc. 2025, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func runAlertDurationValidator(t *testing.T, v alertDurationValidator, input string) *validator.StringResponse {
	t.Helper()

	req := validator.StringRequest{
		Path:        path.Root("duration"),
		ConfigValue: types.StringValue(input),
	}
	resp := &validator.StringResponse{}
	v.ValidateString(context.Background(), req, resp)
	return resp
}

func TestAlertTimeWindowValidatorAcceptsAnyCanonicalDuration(t *testing.T) {
	t.Parallel()

	v := alertDurationValidator{min: alertTimeWindowMin, max: alertTimeWindowMax}

	// "20m" and "2h" are the regression this validator fixes: both are accepted
	// and executed by the API, but the previous preset list rejected them at
	// terraform validate, before any request was made.
	for _, input := range []string{
		"1m", "2m", "5m", "10m", "15m", "20m", "30m", "47m",
		"1h", "1h30m", "2h", "6h", "12h", "24h", "7d", "30d",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if resp := runAlertDurationValidator(t, v, input); resp.Diagnostics.HasError() {
				t.Fatalf("time_window %q was rejected: %v", input, resp.Diagnostics.Errors())
			}
		})
	}
}

func TestAlertFrequencyValidatorAcceptsAnyCanonicalDuration(t *testing.T) {
	t.Parallel()

	v := alertDurationValidator{min: alertFrequencyMin, max: alertFrequencyMax}

	for _, input := range []string{"1m", "3m", "5m", "20m", "30m", "1h", "6h", "12h", "24h"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if resp := runAlertDurationValidator(t, v, input); resp.Diagnostics.HasError() {
				t.Fatalf("frequency %q was rejected: %v", input, resp.Diagnostics.Errors())
			}
		})
	}
}

func TestAlertDurationValidatorRejectsOutOfRange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		v     alertDurationValidator
		input string
	}{
		{name: "frequency below minimum", v: alertDurationValidator{min: alertFrequencyMin, max: alertFrequencyMax}, input: "30s"},
		{name: "frequency above maximum", v: alertDurationValidator{min: alertFrequencyMin, max: alertFrequencyMax}, input: "7d"},
		{name: "time window below minimum", v: alertDurationValidator{min: alertTimeWindowMin, max: alertTimeWindowMax}, input: "30s"},
		{name: "time window above maximum", v: alertDurationValidator{min: alertTimeWindowMin, max: alertTimeWindowMax}, input: "90d"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resp := runAlertDurationValidator(t, tc.v, tc.input)
			if !resp.Diagnostics.HasError() {
				t.Fatalf("expected %q to be rejected", tc.input)
			}
			if summary := resp.Diagnostics.Errors()[0].Summary(); summary != "Duration out of range" {
				t.Fatalf("unexpected summary for %q: %q", tc.input, summary)
			}
		})
	}
}

func TestAlertDurationValidatorRejectsNonCanonicalSpelling(t *testing.T) {
	t.Parallel()

	v := alertDurationValidator{min: alertTimeWindowMin, max: alertTimeWindowMax}

	// Read always rewrites these attributes to their canonical spelling, so an
	// equivalent-but-differently-written value would diff on every plan and
	// never converge. The error must name the spelling to use instead.
	cases := []struct{ input, canonical string }{
		{input: "90m", canonical: "1h30m"},
		{input: "2d", canonical: "48h"},
		{input: "120s", canonical: "2m"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			resp := runAlertDurationValidator(t, v, tc.input)
			if !resp.Diagnostics.HasError() {
				t.Fatalf("expected %q to be rejected as non-canonical", tc.input)
			}
			err := resp.Diagnostics.Errors()[0]
			if err.Summary() != "Non-canonical duration" {
				t.Fatalf("unexpected summary for %q: %q", tc.input, err.Summary())
			}
			if !strings.Contains(err.Detail(), tc.canonical) {
				t.Fatalf("error for %q should name %q, got: %s", tc.input, tc.canonical, err.Detail())
			}
		})
	}
}

func TestAlertDurationValidatorRejectsUnparseable(t *testing.T) {
	t.Parallel()

	v := alertDurationValidator{min: alertTimeWindowMin, max: alertTimeWindowMax}

	for _, input := range []string{"not-a-duration", "", "PT20M"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			resp := runAlertDurationValidator(t, v, input)
			if !resp.Diagnostics.HasError() {
				t.Fatalf("expected %q to be rejected", input)
			}
		})
	}
}

func TestAlertDurationValidatorSkipsNullAndUnknown(t *testing.T) {
	t.Parallel()

	v := alertDurationValidator{min: alertTimeWindowMin, max: alertTimeWindowMax}

	for name, value := range map[string]types.String{
		"null":    types.StringNull(),
		"unknown": types.StringUnknown(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req := validator.StringRequest{Path: path.Root("duration"), ConfigValue: value}
			resp := &validator.StringResponse{}
			v.ValidateString(context.Background(), req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("%s value must not raise an error: %v", name, resp.Diagnostics.Errors())
			}
		})
	}
}

func TestAlertDurationRoundTripsThroughWireFormat(t *testing.T) {
	t.Parallel()

	// Every value the validator accepts must survive the full provider path:
	// config -> durToISO8601 -> API -> iso8601ToDuration -> durationCompact.
	// If it does not, the attribute shows a diff on every plan. "20m" and "2h"
	// are included because the previous preset list rejected them outright.
	for _, input := range []string{"20m", "47m", "2h", "1h30m", "15m", "24h", "7d", "30d"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			d, err := parseDurationStr(types.StringValue(input))
			if err != nil {
				t.Fatalf("parseDurationStr(%q) failed: %v", input, err)
			}

			wire := durToISO8601(d)

			back, err := iso8601ToDuration(wire)
			if err != nil {
				t.Fatalf("iso8601ToDuration(%q) failed: %v", wire, err)
			}

			if got := durationCompact(back); got != input {
				t.Fatalf("%q round-tripped via %q to %q; this would diff on every plan", input, wire, got)
			}
		})
	}
}

// TestAlertSchemaAppliesDurationValidators guards the wiring rather than the
// validator. Every other test here builds an alertDurationValidator directly,
// so all of them would still pass if the schema stopped using it, or used it
// with the wrong bounds. This one pulls the validators off the real resource
// schema and runs them.
func TestAlertSchemaAppliesDurationValidators(t *testing.T) {
	t.Parallel()

	resp := &resource.SchemaResponse{}
	NewAlertResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("building the alert schema failed: %v", resp.Diagnostics.Errors())
	}

	cases := []struct {
		attribute string
		accepts   []string
		rejects   []string
	}{
		{
			attribute: "time_window",
			accepts:   []string{"20m", "2h", "47m", "7d", "30d"},
			rejects:   []string{"30s", "90d", "90m"},
		},
		{
			attribute: "frequency",
			accepts:   []string{"1m", "3m", "20m", "24h"},
			rejects:   []string{"30s", "7d"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.attribute, func(t *testing.T) {
			t.Parallel()

			attr, ok := resp.Schema.Attributes[tc.attribute].(rschema.StringAttribute)
			if !ok {
				t.Fatalf("%s is not a string attribute", tc.attribute)
			}
			if len(attr.Validators) == 0 {
				t.Fatalf("%s has no validators", tc.attribute)
			}

			run := func(input string) diag.Diagnostics {
				var diags diag.Diagnostics
				for _, v := range attr.Validators {
					req := validator.StringRequest{
						Path:        path.Root(tc.attribute),
						ConfigValue: types.StringValue(input),
					}
					vResp := &validator.StringResponse{}
					v.ValidateString(context.Background(), req, vResp)
					diags.Append(vResp.Diagnostics...)
				}
				return diags
			}

			for _, input := range tc.accepts {
				if d := run(input); d.HasError() {
					t.Errorf("schema rejected %s = %q: %v", tc.attribute, input, d.Errors())
				}
			}
			for _, input := range tc.rejects {
				if d := run(input); !d.HasError() {
					t.Errorf("schema accepted %s = %q, expected rejection", tc.attribute, input)
				}
			}
		})
	}
}
