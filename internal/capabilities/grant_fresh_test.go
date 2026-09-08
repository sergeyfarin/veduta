// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import "testing"

func TestFreshGrantDoesNotCarryBudgetAcrossInvocations(t *testing.T) {
	template := NewGrant("p", "1", "i", nil, NewCapSet("log"), nil, nil, nil, Limits{HostCalls: 1}, ExecutionIdentity{})
	first := template.Fresh()
	if err := first.consumeHostCall(); err != nil {
		t.Fatal(err)
	}
	if err := first.consumeHostCall(); err == nil {
		t.Fatal("first invocation exceeded its budget")
	}
	second := template.Fresh()
	if err := second.consumeHostCall(); err != nil {
		t.Fatalf("second invocation inherited spent budget: %v", err)
	}
}
