package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakePendingReviewGitHub(t *testing.T) (string, func(string, any)) {
	t.Helper()
	dir, write := fakePRReviewGitHub(t)
	script := `#!/usr/bin/env python3
import json, pathlib, sys
root=pathlib.Path(__file__).parent
args=sys.argv[1:]
endpoint=next(a for a in args if a.startswith('repos/') or a in ('user','graphql'))
method=args[args.index('--method')+1] if '--method' in args else 'GET'
def read(name,default=None):
 p=root/name
 return json.loads(p.read_text()) if p.exists() else default
def save(name,data): (root/name).write_text(json.dumps(data))
def respond(data): print(json.dumps(data));sys.exit(0)
p=read('pending.json')
body=json.load(sys.stdin) if '--input' in args else None
with (root/'pending-calls').open('a') as out: out.write(json.dumps({'method':method,'endpoint':endpoint,'body':body})+'\n')
if method=='GET':
 if endpoint=='user': respond(read('user.json'))
 if endpoint.endswith('/pulls/12'): respond(read('pr.json'))
 if '/reviews?' in endpoint: respond(([p] if p else [])+read('submitted.json',[])+read('others.json',[]))
 if '/comments?' in endpoint: respond(p.get('comments',[]) if p else [])
 if '/files?' in endpoint: respond(read('files.json'))
 sys.exit(2)
if (root/'reject').exists(): print('gh: Forbidden (HTTP 403)',file=sys.stderr);sys.exit(1)
if method=='POST' and endpoint.endswith('/reviews'):
 assert 'event' not in body, 'must create a pending review'
 assert p is None, 'must reuse an existing pending review'
 p={'id':70,'node_id':'PRR_pending','state':'PENDING','commit_id':body['commit_id'],'user':{'login':'reviewer'},'body':'','comments':[]}
 for c in body['comments']: p['comments'].append(dict(c,id=100+len(p['comments']),node_id='COMMENT_initial'))
 result=p
elif endpoint=='graphql':
 v=body['variables']['input'];assert v['pullRequestReviewId']==p['node_id']
 c={'id':100+len(p['comments']),'node_id':'COMMENT_next','path':v['path'],'line':v['line'],'side':v['side'],'body':v['body']}
 if 'startLine' in v: c.update(start_line=v['startLine'],start_side=v['startSide'])
 p['comments'].append(c);result={'data':{'addPullRequestReviewThread':{'thread':{'id':'THREAD'}}}}
elif method=='PATCH':
 c=next(c for c in p['comments'] if c['id']==int(endpoint.split('/')[-1]));c['body']=body['body'];result=c
elif method=='DELETE':
 p['comments']=[c for c in p['comments'] if c['id']!=int(endpoint.split('/')[-1])];result={}
elif endpoint.endswith('/events'):
 assert int(endpoint.split('/')[-2])==p['id']
 assert 'comments' not in body
 p.update(body=body['body'],state={'COMMENT':'COMMENTED','APPROVE':'APPROVED','REQUEST_CHANGES':'CHANGES_REQUESTED'}[body['event']])
 result=p;save('submitted.json',[p]);p=None
else: sys.exit(3)
save('pending.json',p)
if (root/'disconnect-after').exists(): sys.exit(1)
respond(result)
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return dir, write
}
func pendingChange() PRCommentRequest {
	return PRCommentRequest{Token: "token", RequestID: reviewTestRequest, Action: "add", Comment: testPRReviewSubmission().Comments[0]}
}
func pendingFixture() map[string]any {
	return map[string]any{"id": 70, "node_id": "PRR_pending", "state": "PENDING", "commit_id": reviewTestHead, "user": map[string]string{"login": "reviewer"}, "body": "Summary started on GitHub", "comments": []map[string]any{{"id": 100, "node_id": "COMMENT_existing", "path": "new.go", "line": 2, "side": "RIGHT", "body": "Existing inline feedback"}}}
}
func TestPRReviewPendingCreationReuseAndFinalSummary(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			dir, write := fakePendingReviewGitHub(t)
			s := testPRReviewSnapshot()
			ctx := context.Background()
			if existing {
				write("pending.json", pendingFixture())
			}
			p, err := loadPendingPRReview(ctx, s)
			if err != nil {
				t.Fatal(err)
			}
			if existing && (p == nil || p.Comments[0].Body != "Existing inline feedback") {
				t.Fatal(p)
			}
			req := pendingChange()
			if p != nil {
				req.ReviewID = p.ID
			}
			p, unknown, err := changePendingPRComment(ctx, s, req, false)
			if err != nil || unknown || p == nil {
				t.Fatal(p, unknown, err)
			}
			expected := 1
			if existing {
				expected = 2
			}
			if len(p.Comments) != expected {
				t.Fatal(p)
			}
			callsBefore, _ := os.ReadFile(filepath.Join(dir, "pending-calls"))
			if _, unknown, err = changePendingPRComment(ctx, s, req, false); err != nil || unknown {
				t.Fatal(unknown, err)
			}
			callsAfter, _ := os.ReadFile(filepath.Join(dir, "pending-calls"))
			if strings.Count(string(callsBefore), `"method": "POST"`) != strings.Count(string(callsAfter), `"method": "POST"`) {
				t.Fatal("duplicate write")
			}
			req.RequestID = "second-comment-123456789"
			req.ReviewID = p.ID
			req.Comment.Body = "Second comment"
			p, _, err = changePendingPRComment(ctx, s, req, false)
			if err != nil {
				t.Fatal(err)
			}
			submission := PRReviewSubmission{Token: s.Token, RequestID: "submit-review-123456789", ReviewID: p.ID, PendingVersion: p.Version, Event: "COMMENT", Body: "Final summary"}
			receipt, unknown, err := submitPRReview(ctx, s, submission, false)
			if err != nil || unknown || receipt.ID != p.ID {
				t.Fatal(receipt, unknown, err)
			}
			data, _ := os.ReadFile(filepath.Join(dir, "submitted.json"))
			if !strings.Contains(string(data), "Final summary") || !strings.Contains(string(data), "Second comment") {
				t.Fatal(string(data))
			}
			if existing && !strings.Contains(string(data), "Existing inline feedback") {
				t.Fatal("lost existing review comments")
			}
		})
	}
}
func TestPRReviewPendingEditsConflictsAndLostConfirmation(t *testing.T) {
	dir, write := fakePendingReviewGitHub(t)
	write("pending.json", pendingFixture())
	s := testPRReviewSnapshot()
	ctx := context.Background()
	req := pendingChange()
	req.Action = "update"
	req.ReviewID = 70
	req.CommentID = 100
	req.OriginalBody = "wrong body"
	if _, _, err := changePendingPRComment(ctx, s, req, false); err == nil {
		t.Fatal("overwrote an external edit")
	}
	req.OriginalBody = "Existing inline feedback"
	req.Comment.Body = "Updated feedback"
	if err := os.WriteFile(filepath.Join(dir, "disconnect-after"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, unknown, err := changePendingPRComment(ctx, s, req, false); err == nil || !unknown {
		t.Fatal(unknown, err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "pending-calls"))
	p, unknown, err := changePendingPRComment(ctx, s, req, true)
	if err != nil || unknown || p.Comments[0].Body != "Updated feedback" {
		t.Fatal(p, unknown, err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "pending-calls"))
	if strings.Count(string(after), `"method": "PATCH"`) != strings.Count(string(before), `"method": "PATCH"`) {
		t.Fatal("check repeated PATCH")
	}
	os.Remove(filepath.Join(dir, "disconnect-after"))
	req.Action = "delete"
	req.RequestID = "delete-comment-123456789"
	req.OriginalBody = "Updated feedback"
	p, _, err = changePendingPRComment(ctx, s, req, false)
	if err != nil || len(p.Comments) != 0 {
		t.Fatal(p, err)
	}
}
func TestPRReviewPendingOwnershipAndChangedRevision(t *testing.T) {
	for _, kind := range []string{"account", "head", "review", "comments"} {
		t.Run(kind, func(t *testing.T) {
			dir, write := fakePendingReviewGitHub(t)
			write("pending.json", pendingFixture())
			s := testPRReviewSnapshot()
			ctx := context.Background()
			p, err := loadPendingPRReview(ctx, s)
			if err != nil {
				t.Fatal(err)
			}
			req := pendingChange()
			req.ReviewID = p.ID
			switch kind {
			case "account":
				write("user.json", map[string]string{"login": "someone-else"})
			case "head":
				s.Head = strings.Repeat("c", 40)
			case "review":
				req.ReviewID = 999
			}
			if kind == "comments" {
				q := pendingFixture()
				q["body"] = "Changed remotely"
				write("pending.json", q)
				_, _, err = submitPRReview(ctx, s, PRReviewSubmission{Token: s.Token, RequestID: reviewTestRequest, ReviewID: p.ID, PendingVersion: p.Version, Event: "COMMENT"}, false)
			} else {
				_, _, err = changePendingPRComment(ctx, s, req, false)
			}
			if err == nil {
				t.Fatal("accepted changed review")
			}
			data, _ := os.ReadFile(filepath.Join(dir, "pending-calls"))
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var call map[string]any
				json.Unmarshal([]byte(line), &call)
				if call["method"] != "GET" {
					t.Fatal(line)
				}
			}
		})
	}
}
