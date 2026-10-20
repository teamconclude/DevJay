package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Present only when GitHub confirms this PR belongs to someone other than the
// authenticated viewer. Missing metadata must not imply someone else's PR.
type PullRequestAuthor struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatarURL,omitempty"`
}

var githubAuthorLogin = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}(\[bot\])?$`)

func githubAvatarURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "avatars.githubusercontent.com" || u.User != nil || u.Fragment != "" {
		return ""
	}
	return u.String()
}

func otherPullRequestAuthors(data []byte, prs []PullRequestLink) map[int]*PullRequestAuthor {
	var response struct {
		Data struct {
			Repository map[string]*struct {
				Number          int
				ViewerDidAuthor *bool
				Author          *struct{ Login, AvatarURL string }
			}
		}
		Errors []json.RawMessage
	}
	if json.Unmarshal(data, &response) != nil || len(response.Errors) != 0 {
		return nil
	}
	authors := map[int]*PullRequestAuthor{}
	for i, pr := range prs {
		item := response.Data.Repository[fmt.Sprintf("pr%d", i)]
		if item == nil || item.Number != pr.Number || item.ViewerDidAuthor == nil || *item.ViewerDidAuthor || item.Author == nil || !githubAuthorLogin.MatchString(item.Author.Login) {
			continue
		}
		authors[pr.Number] = &PullRequestAuthor{Login: item.Author.Login, AvatarURL: githubAvatarURL(item.Author.AvatarURL)}
	}
	return authors
}

// Runs only in the full background refresh. Ownership and avatar come from the
// same authenticated query, avoiding a separate account lookup or guessed URL.
func refreshPullRequestAuthors(ctx context.Context, repo string, prs []PullRequestLink) {
	if len(prs) == 0 || len(prs) > 20 {
		return
	}
	var query strings.Builder
	query.WriteString("query($owner:String!,$name:String!){repository(owner:$owner,name:$name){")
	for i, pr := range prs {
		link, valid := pullRequestLink(repo, pr.Number, pr.State, pr.IsDraft)
		if !valid || pr.URL != link.URL {
			return
		}
		fmt.Fprintf(&query, "pr%d:pullRequest(number:%d){number viewerDidAuthor author{login avatarUrl(size:48)}}", i, pr.Number)
	}
	query.WriteString("}}")
	owner, name, _ := strings.Cut(repo, "/")
	authorCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := runCommand(authorCtx, 10*time.Second, "gh", "api", "graphql", "-f", "query="+query.String(), "-f", "owner="+owner, "-f", "name="+name)
	if err != nil {
		return
	}
	authors := otherPullRequestAuthors(out, prs)
	for i := range prs {
		prs[i].OtherAuthor = authors[prs[i].Number]
	}
}
