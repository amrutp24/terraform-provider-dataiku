package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// suppressEquivalentJSON keeps a reformatted JSON document from planning as a
// change. jsontypes.Normalized gives an attribute semantic equality, which
// stops "provider produced inconsistent result" errors, but Terraform still
// diffs the literal strings when building a plan, so indentation or key order
// alone would otherwise show up as an update.
type suppressEquivalentJSON struct{}

func suppressEquivalentJSONPlanModifier() planmodifier.String {
	return suppressEquivalentJSON{}
}

func (m suppressEquivalentJSON) Description(_ context.Context) string {
	return "Suppresses a diff when the configured JSON is equivalent to what is already stored."
}

func (m suppressEquivalentJSON) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m suppressEquivalentJSON) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Nothing to compare against on create, destroy, or while the value is
	// still being computed.
	if req.StateValue.IsNull() || req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}

	if jsonEquivalent(req.StateValue.ValueString(), req.PlanValue.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}

// jsonEquivalent reports whether two documents differ only in formatting. Any
// document that does not parse is treated as not equivalent, which leaves the
// diff in place and lets the attribute's own validation report the problem.
func jsonEquivalent(a, b string) bool {
	canonicalA, ok := canonicalJSON(a)
	if !ok {
		return false
	}
	canonicalB, ok := canonicalJSON(b)
	if !ok {
		return false
	}
	return canonicalA == canonicalB
}

func canonicalJSON(s string) (string, bool) {
	var parsed any
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		return "", false
	}
	// encoding/json sorts object keys, so re-marshalling normalises both
	// whitespace and key order.
	encoded, err := json.Marshal(parsed)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}
