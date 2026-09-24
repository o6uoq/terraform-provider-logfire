// Copyright Pydantic, Inc. 2025, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// alertDurationValidator rejects a duration the API rejects: one that does not
// parse, or one outside the field's absolute bounds.
//
// The API does not expose an enum for these fields. It accepts an arbitrary
// ISO-8601 duration inside a range, and separately enforces a relationship
// between time_window and frequency (a longer window requires a less frequent
// evaluation). That relationship is server-side policy, so it is left to the
// API to report; mirroring it here would drift the moment the server changes.
//
// Equivalent spellings are accepted. The read path rewrites the attribute to
// its compact form, and alertDurationType's semantic equality keeps the
// configured spelling when both mean the same duration.
type alertDurationValidator struct {
	min time.Duration
	max time.Duration
}

func (v alertDurationValidator) Description(_ context.Context) string {
	return fmt.Sprintf("must be a duration between %s and %s", durationCompact(v.min), durationCompact(v.max))
}

func (v alertDurationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v alertDurationValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	raw := req.ConfigValue.ValueString()

	d, err := parseDurationText(raw)
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
	}
}
