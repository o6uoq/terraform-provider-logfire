// Copyright Pydantic, Inc. 2025, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// alertDurationValidator accepts any duration the Logfire API accepts for an
// alert, rather than a fixed list of presets.
//
// The API does not expose an enum for these fields. It accepts an arbitrary
// ISO-8601 duration inside a range, and separately enforces a relationship
// between time_window and frequency (a longer window requires a less frequent
// evaluation). That relationship is server-side policy, so it is left to the
// API to report; mirroring it here would drift the moment the server changes.
//
// The validator therefore checks only what is stable and provable client-side:
//
//  1. the value parses as a duration;
//  2. it is positive and within the field's absolute bounds;
//  3. it is written in the canonical form the provider emits when it reads an
//     alert back (durationCompact).
//
// Rule 3 matters. Read always rewrites these attributes to their canonical
// spelling, so a config holding an equivalent-but-differently-spelled value
// ("90m" for "1h30m", "2d" for "48h") would produce a diff on every plan that
// never converges. Rejecting it during validate turns a confusing permanent
// diff into one actionable error naming the spelling to use.
type alertDurationValidator struct {
	min time.Duration
	max time.Duration
}

func (v alertDurationValidator) Description(_ context.Context) string {
	return fmt.Sprintf(
		"must be a duration between %s and %s, written canonically (for example 30s, 20m, 1h30m, 7d)",
		durationCompact(v.min), durationCompact(v.max),
	)
}

func (v alertDurationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v alertDurationValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	raw := strings.TrimSpace(req.ConfigValue.ValueString())

	d, err := parseDurationStr(req.ConfigValue)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid duration",
			fmt.Sprintf("%q is not a duration. Use a Go duration such as %q, or day shorthand such as %q.", raw, "20m", "7d"),
		)
		return
	}

	if d < v.min || d > v.max {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Duration out of range",
			fmt.Sprintf(
				"%q is %s, outside the supported range %s to %s.",
				raw, durationCompact(d), durationCompact(v.min), durationCompact(v.max),
			),
		)
		return
	}

	if canonical := durationCompact(d); canonical != raw {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Non-canonical duration",
			fmt.Sprintf(
				"%q means the same as %q, but the provider always reads this attribute back as %q, "+
					"so %q would show a diff on every plan. Use %q.",
				raw, canonical, canonical, raw, canonical,
			),
		)
	}
}
