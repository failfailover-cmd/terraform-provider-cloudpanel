package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// fakeSSM answers each SendCommand with the next scripted invocation results.
type fakeSSM struct {
	sent    []*ssm.SendCommandInput
	results map[string][]ssm.GetCommandInvocationOutput // by document
	pending map[string][]ssm.GetCommandInvocationOutput // by command id
	notYet  int                                         // InvocationDoesNotExist answers before the first result
}

func (f *fakeSSM) SendCommand(_ context.Context, in *ssm.SendCommandInput, _ ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
	f.sent = append(f.sent, in)
	id := aws.ToString(in.DocumentName) + "#" + string(rune('0'+len(f.sent)))
	if f.pending == nil {
		f.pending = map[string][]ssm.GetCommandInvocationOutput{}
	}
	f.pending[id] = f.results[aws.ToString(in.DocumentName)]
	return &ssm.SendCommandOutput{Command: &ssmtypes.Command{CommandId: aws.String(id)}}, nil
}

func (f *fakeSSM) GetCommandInvocation(_ context.Context, in *ssm.GetCommandInvocationInput, _ ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
	if f.notYet > 0 {
		f.notYet--
		return nil, &ssmtypes.InvocationDoesNotExist{}
	}
	id := aws.ToString(in.CommandId)
	q := f.pending[id]
	out := q[0]
	if len(q) > 1 {
		f.pending[id] = q[1:]
	}
	return &out, nil
}

func ok(stdout string) ssm.GetCommandInvocationOutput {
	return ssm.GetCommandInvocationOutput{Status: ssmtypes.CommandInvocationStatusSuccess, StandardOutputContent: aws.String(stdout)}
}

func newTestClient(f *fakeSSM) *client {
	return &client{ssm: f, instanceID: "i-test", pollInterval: time.Millisecond, timeout: time.Second}
}

const listJSON = `[{"domain_name": "a.com", "type": "static", "user": "a-com", "root_directory": "a.com"}]`

func TestListSitesCachedAndInvalidated(t *testing.T) {
	f := &fakeSSM{notYet: 2, results: map[string][]ssm.GetCommandInvocationOutput{
		docList:      {{Status: ssmtypes.CommandInvocationStatusInProgress}, ok(listJSON)},
		docAddStatic: {ok("Site b.com has been added.")},
	}}
	c := newTestClient(f)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		sites, err := c.listSites(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if sites["a.com"].User != "a-com" {
			t.Fatalf("unexpected sites: %+v", sites)
		}
	}
	if len(f.sent) != 1 {
		t.Fatalf("list must be sent once per run, sent %d", len(f.sent))
	}

	if err := c.addStaticSite(ctx, "b.com", "b-com"); err != nil {
		t.Fatal(err)
	}
	add := f.sent[1]
	if aws.ToString(add.DocumentName) != docAddStatic || add.Parameters["domainName"][0] != "b.com" || add.Parameters["siteUser"][0] != "b-com" || add.InstanceIds[0] != "i-test" {
		t.Fatalf("unexpected add command: %+v", add)
	}
	if _, err := c.listSites(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 3 {
		t.Fatalf("list must be re-sent after a change, sent %d", len(f.sent))
	}
}

func TestRunReportsFailure(t *testing.T) {
	f := &fakeSSM{results: map[string][]ssm.GetCommandInvocationOutput{
		docDelete: {{Status: ssmtypes.CommandInvocationStatusFailed, StandardErrorContent: aws.String("Site not found")}},
	}}
	err := newTestClient(f).deleteSite(context.Background(), "x.com")
	if err == nil || !strings.Contains(err.Error(), "Site not found") {
		t.Fatalf("expected failure with stderr, got %v", err)
	}
}

func TestListRejectsTruncatedOutput(t *testing.T) {
	f := &fakeSSM{results: map[string][]ssm.GetCommandInvocationOutput{
		docList: {ok(strings.Repeat("x", maxInlineOutput))},
	}}
	if _, err := newTestClient(f).listSites(context.Background()); err == nil || !strings.Contains(err.Error(), "inline limit") {
		t.Fatalf("expected truncation error, got %v", err)
	}
}
