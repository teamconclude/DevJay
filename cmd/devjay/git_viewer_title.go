package main

import "fmt"

func gitViewerTitle(name, kind, branch string, status GitStatus) string {
	if kind != "review" {
		return "DevJay · " + name + " · Changes"
	}
	label := "Review branch"
	if branch != "" && branch == status.Branch {
		for _, pr := range status.PullRequests {
			if pr.Number > 0 {
				label = fmt.Sprintf("Review PR %d", pr.Number)
				break
			}
		}
	}
	return "DevJay - " + name + " - " + label
}
