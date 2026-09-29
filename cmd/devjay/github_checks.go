package main

import "encoding/json"

// gh's exported statusCheckRollup belongs to the PR's latest pushed commit and
// combines Actions check runs with legacy commit-status contexts. It contains
// at most the first 100 contexts without pagination metadata. An observed
// failure is useful even in a partial result; a full page cannot prove success.
func pullRequestCIState(pr pullRequest) string {
	if pr.HeadRefOid == "" {
		return "unknown"
	}
	var checks []json.RawMessage
	if json.Unmarshal(pr.StatusCheckRollup, &checks) != nil || checks == nil {
		return "unknown"
	}
	if len(checks) == 0 {
		return "none"
	}
	states := map[string]bool{}
	for _, check := range checks {
		state := pullRequestCheckState(check)
		if state == "failure" {
			return state
		}
		states[state] = true
	}
	if states["action_required"] {
		return "action_required"
	}
	if states["pending"] {
		return "pending"
	}
	if states["unknown"] || len(checks) >= 100 {
		return "unknown"
	}
	if states["cancelled"] {
		return "cancelled"
	}
	if states["success"] {
		return "success"
	}
	return "neutral"
}

func pullRequestCheckState(data json.RawMessage) string {
	var check struct {
		TypeName   string `json:"__typename"`
		Status     string
		Conclusion string
		State      string
	}
	if json.Unmarshal(data, &check) != nil {
		return "unknown"
	}
	switch check.TypeName {
	case "StatusContext":
		switch check.State {
		case "FAILURE", "ERROR":
			return "failure"
		case "EXPECTED", "PENDING":
			return "pending"
		case "SUCCESS":
			return "success"
		}
	case "CheckRun":
		switch check.Status {
		case "QUEUED", "IN_PROGRESS", "PENDING", "REQUESTED", "WAITING":
			return "pending"
		case "COMPLETED":
			switch check.Conclusion {
			case "FAILURE", "TIMED_OUT", "STARTUP_FAILURE":
				return "failure"
			case "ACTION_REQUIRED":
				return "action_required"
			case "CANCELLED":
				return "cancelled"
			case "SUCCESS":
				return "success"
			case "NEUTRAL", "SKIPPED":
				return "neutral"
			}
		}
	}
	return "unknown"
}
